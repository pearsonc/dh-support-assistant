package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pearsonc/dh-support-assistant/internal/domain"
)

// Summary is the per-run accounting emitted by the orchestrator as JSON
// on stdout. Fields match the Phase 1 plan's acceptance spec exactly:
// tickets_new, tickets_updated, events_new, events_skipped, elapsed_ms.
//
// ShortCircuited is internal-only (json:"-") and lets the orchestrator
// distinguish the two distinct paths that populate EventsSkipped:
//   - short-circuit (import_hash already seen): Write never walks
//     individual events, and EventsSkipped is the full journal count from
//     the file. Not worth a warn line — this is the expected re-ingest
//     outcome the user triggered.
//   - per-event DO NOTHING against ticket_events_idem_key: the writer
//     saw at least one (ticket, ts, author, body_hash) already present
//     in the DB. Worth a warn line so the operator notices dedup activity
//     trending up over time.
type Summary struct {
	TicketsNew     int   `json:"tickets_new"`
	TicketsUpdated int   `json:"tickets_updated"`
	EventsNew      int   `json:"events_new"`
	EventsSkipped  int   `json:"events_skipped"`
	ElapsedMS      int64 `json:"elapsed_ms"`
	ShortCircuited bool  `json:"-"`
}

// Write ingests records under a single transaction. Idempotency is
// enforced at three levels per [Rule: Ingest idempotency]:
//   - imports.import_hash UNIQUE short-circuits a re-run of the same file
//     and fills events_skipped with the full journal count from the file
//     so the operator can see that events were recognised but deduplicated.
//   - tickets.ticket_external_id UNIQUE turns a repeat ticket into a
//     DO UPDATE and distinguishes TicketsNew vs TicketsUpdated via the
//     Postgres xmax idiom (xmax = 0 ⇔ this row was inserted, not updated).
//   - ticket_events (ticket_id, event_ts, author_name, body_hash) UNIQUE
//     DO NOTHING discards any journal row re-observed from a later export.
//     body_hash is part of the key because ServiceNow workflow automation
//     emits multiple distinct-body entries inside a single second for the
//     same ticket/author — collapsing on (ticket_id, event_ts, author_name)
//     alone lost 8.3% of events in the Phase 0 sample (see 0006 migration).
//
// On short-circuit Commit is still called so the transaction closes cleanly;
// no writes occurred.
func Write(ctx context.Context, pool *pgxpool.Pool, hash, fileName string, records []Record) (Summary, error) {
	startedAt := time.Now()
	totalEvents := countJournalEntries(records)

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Summary{}, fmt.Errorf("ingest: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	importID, alreadyImported, err := upsertImport(ctx, tx, hash, fileName, len(records))
	if err != nil {
		return Summary{}, err
	}

	var s Summary
	if alreadyImported {
		s.EventsSkipped = totalEvents
		s.ShortCircuited = true
		if err := tx.Commit(ctx); err != nil {
			return s, fmt.Errorf("ingest: commit short-circuit: %w", err)
		}
		s.ElapsedMS = time.Since(startedAt).Milliseconds()
		return s, nil
	}

	for i := range records {
		rec := &records[i]
		clientID, err := upsertClient(ctx, tx, rec.BusinessService.ClientName)
		if err != nil {
			return s, err
		}
		bsID, err := upsertBusinessService(ctx, tx, rec.BusinessService, clientID)
		if err != nil {
			return s, err
		}
		res, err := upsertTicket(ctx, tx, rec, bsID, importID)
		if err != nil {
			return s, err
		}
		if res.New {
			s.TicketsNew++
		} else {
			s.TicketsUpdated++
		}
		newEvents, skippedEvents, err := insertEvents(ctx, tx, res.ID, importID, rec.Journal)
		if err != nil {
			return s, err
		}
		s.EventsNew += newEvents
		s.EventsSkipped += skippedEvents
	}

	if err := tx.Commit(ctx); err != nil {
		return s, fmt.Errorf("ingest: commit: %w", err)
	}
	s.ElapsedMS = time.Since(startedAt).Milliseconds()
	return s, nil
}

func countJournalEntries(records []Record) int {
	n := 0
	for _, r := range records {
		n += len(r.Journal)
	}
	return n
}

const upsertImportSQL = `
INSERT INTO imports (import_hash, file_name, row_count)
VALUES ($1, $2, $3)
ON CONFLICT (import_hash) DO NOTHING
RETURNING id
`

const selectImportByHashSQL = `SELECT id FROM imports WHERE import_hash = $1`

func upsertImport(ctx context.Context, tx pgx.Tx, hash, fileName string, rowCount int) (int64, bool, error) {
	var id int64
	err := tx.QueryRow(ctx, upsertImportSQL, hash, fileName, rowCount).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, selectImportByHashSQL, hash).Scan(&id); err != nil {
			return 0, false, fmt.Errorf("ingest: select existing import: %w", err)
		}
		return id, true, nil
	case err != nil:
		return 0, false, fmt.Errorf("ingest: insert import: %w", err)
	}
	return id, false, nil
}

const upsertClientSQL = `
INSERT INTO clients (client_name)
VALUES ($1)
ON CONFLICT (client_name) DO UPDATE SET client_name = EXCLUDED.client_name
RETURNING id
`

func upsertClient(ctx context.Context, tx pgx.Tx, clientName *string) (*int64, error) {
	if clientName == nil {
		return nil, nil
	}
	var id int64
	if err := tx.QueryRow(ctx, upsertClientSQL, *clientName).Scan(&id); err != nil {
		return nil, fmt.Errorf("ingest: upsert client %q: %w", *clientName, err)
	}
	return &id, nil
}

const upsertBusinessServiceSQL = `
INSERT INTO business_services (raw_value, platform, client_id, country_code, product)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (raw_value) DO UPDATE SET raw_value = EXCLUDED.raw_value
RETURNING id
`

func upsertBusinessService(ctx context.Context, tx pgx.Tx, bs domain.BusinessService, clientID *int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, upsertBusinessServiceSQL,
		bs.RawValue, bs.Platform, clientID, bs.CountryCode, bs.Product,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ingest: upsert business service %q: %w", bs.RawValue, err)
	}
	return id, nil
}

// (xmax = 0) AS new_row is a Postgres idiom: xmax is unset on freshly
// INSERTed rows and set to the updating txid on rows that hit DO UPDATE,
// so it tells us insert-vs-update without a second query or an advisory
// tag.
const upsertTicketSQL = `
INSERT INTO tickets (
    ticket_external_id, short_description, description,
    state, priority, severity, urgency, impact,
    assigned_to, assignment_group,
    opened_at, updated_at, opened_by, updated_by, caller, company_raw,
    business_service_id, created_at, created_by, category, subcategory, due_date,
    jira_id, jira_key, jira_url, jira_project, jira_status,
    source_import_id
)
VALUES (
    $1, $2, $3,
    $4, $5, $6, $7, $8,
    $9, $10,
    $11, $12, $13, $14, $15, $16,
    $17, $18, $19, $20, $21, $22,
    $23, $24, $25, $26, $27,
    $28
)
ON CONFLICT (ticket_external_id) DO UPDATE SET
    short_description = EXCLUDED.short_description,
    description = EXCLUDED.description,
    state = EXCLUDED.state,
    priority = EXCLUDED.priority,
    severity = EXCLUDED.severity,
    urgency = EXCLUDED.urgency,
    impact = EXCLUDED.impact,
    assigned_to = EXCLUDED.assigned_to,
    assignment_group = EXCLUDED.assignment_group,
    opened_at = EXCLUDED.opened_at,
    updated_at = EXCLUDED.updated_at,
    opened_by = EXCLUDED.opened_by,
    updated_by = EXCLUDED.updated_by,
    caller = EXCLUDED.caller,
    company_raw = EXCLUDED.company_raw,
    business_service_id = EXCLUDED.business_service_id,
    created_at = EXCLUDED.created_at,
    created_by = EXCLUDED.created_by,
    category = EXCLUDED.category,
    subcategory = EXCLUDED.subcategory,
    due_date = EXCLUDED.due_date,
    jira_id = EXCLUDED.jira_id,
    jira_key = EXCLUDED.jira_key,
    jira_url = EXCLUDED.jira_url,
    jira_project = EXCLUDED.jira_project,
    jira_status = EXCLUDED.jira_status,
    source_import_id = EXCLUDED.source_import_id
RETURNING id, (xmax = 0) AS new_row
`

type upsertTicketResult struct {
	ID  int64
	New bool
}

func upsertTicket(ctx context.Context, tx pgx.Tx, r *Record, businessServiceID, importID int64) (upsertTicketResult, error) {
	var res upsertTicketResult
	err := tx.QueryRow(ctx, upsertTicketSQL,
		r.TicketExternalID, r.ShortDescription, r.Description,
		r.State, r.Priority, r.Severity, r.Urgency, r.Impact,
		r.AssignedTo, r.AssignmentGroup,
		r.OpenedAt, r.UpdatedAt, r.OpenedBy, r.UpdatedBy, r.Caller, r.CompanyRaw,
		businessServiceID, r.CreatedAt, r.CreatedBy, r.Category, r.Subcategory, r.DueDate,
		r.JiraID, r.JiraKey, r.JiraURL, r.JiraProject, r.JiraStatus,
		importID,
	).Scan(&res.ID, &res.New)
	if err != nil {
		return upsertTicketResult{}, fmt.Errorf("ingest: upsert ticket %q: %w", r.TicketExternalID, err)
	}
	return res, nil
}

// body_hash is computed server-side with md5($5) so the writer and the
// 0006 migration's ADD COLUMN DEFAULT md5(body) share one expression — no
// client-side hashing drift. The ON CONFLICT tuple matches the
// ticket_events_idem_key UNIQUE constraint added by 0006.
const insertEventSQL = `
INSERT INTO ticket_events (ticket_id, event_ts, author_name, author_context, body, body_hash, source_import_id)
VALUES ($1, $2, $3, $4, $5, md5($5), $6)
ON CONFLICT (ticket_id, event_ts, author_name, body_hash) DO NOTHING
`

func insertEvents(ctx context.Context, tx pgx.Tx, ticketID, importID int64, entries []domain.JournalEntry) (int, int, error) {
	newCount, skippedCount := 0, 0
	for _, e := range entries {
		tag, err := tx.Exec(ctx, insertEventSQL,
			ticketID, e.EventTS, e.AuthorName, e.AuthorContext, e.Body, importID,
		)
		if err != nil {
			return newCount, skippedCount, fmt.Errorf("ingest: insert event ticket=%d ts=%s author=%q: %w",
				ticketID, e.EventTS.Format(time.RFC3339), e.AuthorName, err)
		}
		if tag.RowsAffected() == 1 {
			newCount++
		} else {
			skippedCount++
		}
	}
	return newCount, skippedCount, nil
}
