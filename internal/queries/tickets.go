package queries

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Sort tokens accepted by ListTickets. Kept as a closed whitelist because
// ORDER BY cannot be parameterised — the token is interpolated as a literal
// after whitelist check. Adding a new sort means adding BOTH the constant
// and an entry in sortClauses.
const (
	SortUpdatedAtDesc = "updated_at_desc"
	SortOpenedAtDesc  = "opened_at_desc"
	SortDueDateAsc    = "due_date_asc"
)

// sortClauses maps a safe-listed sort token to the literal SQL fragment
// spliced into ListTickets' ORDER BY. Never build ORDER BY from user input
// via string concat — only via this map lookup.
var sortClauses = map[string]string{
	SortUpdatedAtDesc: "t.updated_at DESC",
	SortOpenedAtDesc:  "t.opened_at DESC",
	SortDueDateAsc:    "t.due_date ASC",
}

// DefaultListLimit is applied when the caller omits limit. MaxListLimit is
// the ceiling — request for 10000 gets capped at this value to keep a single
// handler from draining the pool.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// ListTicketsParams carries parsed query string values into ListTickets.
// Empty Severity / State mean "no filter" (not "filter by empty string").
type ListTicketsParams struct {
	Limit    int
	Offset   int
	Sort     string
	Severity string
	State    string
}

// ErrUnknownSort is returned when Params.Sort isn't in sortClauses. The
// handler layer maps this to 400 Bad Request.
var ErrUnknownSort = errors.New("queries: unknown sort token")

// Normalise clamps Limit into [1, MaxListLimit], defaults empty Sort to the
// latest-activity order, and rejects unknown sort tokens. Call once in the
// handler before passing the struct down.
func (p *ListTicketsParams) Normalise() error {
	if p.Limit <= 0 {
		p.Limit = DefaultListLimit
	}
	if p.Limit > MaxListLimit {
		p.Limit = MaxListLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Sort == "" {
		p.Sort = SortUpdatedAtDesc
	}
	if _, ok := sortClauses[p.Sort]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSort, p.Sort)
	}
	return nil
}

// listTicketsSelect is the shared projection for list + detail queries.
// Joins business_services + clients so the UI can render client name /
// country in the queue without a second lookup. LEFT JOIN clients because
// internal services (dunnhumby) have client_id = NULL.
const listTicketsSelect = `
SELECT
  t.id, t.ticket_external_id, t.short_description, t.state, t.severity,
  t.priority, t.assigned_to, t.assignment_group, t.opened_at, t.updated_at,
  t.due_date, t.business_service_id,
  bs.client_id, c.client_name, bs.country_code, bs.product
FROM tickets t
JOIN business_services bs ON bs.id = t.business_service_id
LEFT JOIN clients c ON c.id = bs.client_id
WHERE ($1 = '' OR t.severity = $1)
  AND ($2 = '' OR t.state = $2)
`

// countTicketsSQL runs with the same WHERE clause as listTicketsSelect so
// the Total returned to the client matches the filtered-but-unpaginated row
// count. The JOIN is kept (rather than counting tickets in isolation) in
// case future filters key on the joined columns.
const countTicketsSQL = `
SELECT COUNT(*)
FROM tickets t
JOIN business_services bs ON bs.id = t.business_service_id
LEFT JOIN clients c ON c.id = bs.client_id
WHERE ($1 = '' OR t.severity = $1)
  AND ($2 = '' OR t.state = $2)
`

// ListTickets returns one page of tickets plus the total matching rows.
// params MUST be Normalise'd first. Runs two queries: the filtered page
// and a COUNT(*). Both share the same WHERE clause so Total reflects the
// filtered set, not the global table size.
func (q *Queries) ListTickets(ctx context.Context, params ListTicketsParams) (TicketListResult, error) {
	orderBy, ok := sortClauses[params.Sort]
	if !ok {
		return TicketListResult{}, fmt.Errorf("list tickets: %w: %q", ErrUnknownSort, params.Sort)
	}

	sqlText := listTicketsSelect + " ORDER BY " + orderBy + " LIMIT $3 OFFSET $4"
	rows, err := q.pool.Query(ctx, sqlText, params.Severity, params.State, params.Limit, params.Offset)
	if err != nil {
		return TicketListResult{}, fmt.Errorf("list tickets query: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[TicketListItem])
	if err != nil {
		return TicketListResult{}, fmt.Errorf("list tickets collect: %w", err)
	}

	var total int
	if err := q.pool.QueryRow(ctx, countTicketsSQL, params.Severity, params.State).Scan(&total); err != nil {
		return TicketListResult{}, fmt.Errorf("list tickets count: %w", err)
	}

	return TicketListResult{Items: items, Total: total}, nil
}

// ErrTicketNotFound is returned when GetTicket is asked for an id that
// doesn't exist. Handler layer maps this to 404.
var ErrTicketNotFound = errors.New("queries: ticket not found")

const ticketDetailSQL = `
SELECT
  t.id, t.ticket_external_id, t.short_description, t.description,
  t.state, t.priority, t.severity, t.urgency, t.impact,
  t.assigned_to, t.assignment_group,
  t.opened_at, t.updated_at, t.opened_by, t.updated_by,
  t.caller, t.company_raw, t.business_service_id,
  t.created_at, t.created_by,
  t.category, t.subcategory, t.due_date,
  t.jira_id, t.jira_key, t.jira_url, t.jira_project, t.jira_status,
  bs.client_id, c.client_name, bs.country_code, bs.product
FROM tickets t
JOIN business_services bs ON bs.id = t.business_service_id
LEFT JOIN clients c ON c.id = bs.client_id
WHERE t.id = $1
`

const ticketEventsSQL = `
SELECT id, event_ts, author_name, author_context, body
FROM ticket_events
WHERE ticket_id = $1
ORDER BY event_ts DESC, id DESC
`

// GetTicket fetches a single ticket and its full timeline. Returns
// ErrTicketNotFound if the id doesn't exist. Events are sorted newest-first
// by event_ts then id (id tie-break covers multi-entry-same-second rows
// from the Phase 1 body_hash idempotency domain knowledge).
func (q *Queries) GetTicket(ctx context.Context, id int64) (TicketDetailResult, error) {
	rows, err := q.pool.Query(ctx, ticketDetailSQL, id)
	if err != nil {
		return TicketDetailResult{}, fmt.Errorf("get ticket query: %w", err)
	}
	ticket, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[TicketDetail])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TicketDetailResult{}, fmt.Errorf("get ticket %d: %w", id, ErrTicketNotFound)
		}
		return TicketDetailResult{}, fmt.Errorf("get ticket collect: %w", err)
	}

	eventRows, err := q.pool.Query(ctx, ticketEventsSQL, id)
	if err != nil {
		return TicketDetailResult{}, fmt.Errorf("get ticket events query: %w", err)
	}
	events, err := pgx.CollectRows(eventRows, pgx.RowToStructByName[TicketEventItem])
	if err != nil {
		return TicketDetailResult{}, fmt.Errorf("get ticket events collect: %w", err)
	}

	return TicketDetailResult{Ticket: ticket, Events: events}, nil
}
