//go:build integration

package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/pearsonc/dh-support-assistant/internal/db"
	"github.com/pearsonc/dh-support-assistant/internal/ingest"
	"github.com/pearsonc/dh-support-assistant/internal/ingest/fixtures"
)

// pgvectorImage mirrors the pin used in docker/compose.yaml so the test
// runs the byte-identical postgres image that production ingests against.
// Updating the compose image MUST update this constant in lockstep so
// schema/behaviour divergence cannot slip past the integration test.
const pgvectorImage = "pgvector/pgvector:pg16@sha256:7d400e340efb42f4d8c9c12c6427adb253f726881a9985d2a471bf0eed824dff"

// TestIngestIntegration runs the full ingest pipeline against a fresh
// testcontainer-managed Postgres. It is the primary gate for sub-phase 1.5
// per the Phase 1 plan: migrations apply, fixture ingests correctly,
// re-ingest short-circuits (no warn log), and a modified re-ingest
// deduplicates on body_hash and emits the operator warn line exactly once.
//
// The build tag `integration` keeps this out of the default `make test`
// run. Execute with `make test-integration` or `go test -tags integration
// ./internal/ingest/...`. Requires Docker reachable by the host.
func TestIngestIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, pgvectorImage,
		postgres.WithDatabase("dh_test"),
		postgres.WithUsername("dh"),
		postgres.WithPassword("dh"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		// Terminate with its own context so the stop isn't cancelled by
		// the test-wide context timeout.
		ctxCleanup, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()
		if err := container.Terminate(ctxCleanup); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := db.NewPool(ctx, connStr)
	if err != nil {
		t.Fatalf("db pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Silent logger for migration bring-up; the ingest calls get a log
	// capture buffer so the warn-line assertion can inspect emission.
	quietLogger := zerolog.New(io.Discard)
	if err := db.MigrateUp(ctx, pool, quietLogger); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	t.Run("fresh_ingest", func(t *testing.T) {
		fixturePath := fixtures.WriteSample10(t)

		var logBuf bytes.Buffer
		logger := zerolog.New(&logBuf)

		summary, err := ingest.Run(ctx, pool, logger, fixturePath)
		if err != nil {
			t.Fatalf("ingest.Run: %v", err)
		}
		if summary.TicketsNew != 10 || summary.TicketsUpdated != 0 ||
			summary.EventsNew != 12 || summary.EventsSkipped != 0 ||
			summary.ShortCircuited {
			t.Errorf("fresh summary: got %+v; want {new:10 upd:0 ev_new:12 ev_skip:0 short:false}", summary)
		}

		assertRowCounts(t, ctx, pool, fixtures.ExpectedSample10Counts())
		assertEventsAscending(t, ctx, pool)

		// Fresh run must not emit the warn line (EventsSkipped=0).
		if warnCount := countLogLevel(t, logBuf.Bytes(), "warn"); warnCount != 0 {
			t.Errorf("fresh run warn count: got %d, want 0", warnCount)
		}
	})

	t.Run("reingest_short_circuits", func(t *testing.T) {
		fixturePath := fixtures.WriteSample10(t)

		var logBuf bytes.Buffer
		logger := zerolog.New(&logBuf)

		summary, err := ingest.Run(ctx, pool, logger, fixturePath)
		if err != nil {
			t.Fatalf("ingest.Run re-ingest: %v", err)
		}
		if summary.TicketsNew != 0 || summary.TicketsUpdated != 0 ||
			summary.EventsNew != 0 || summary.EventsSkipped != 12 ||
			!summary.ShortCircuited {
			t.Errorf("re-ingest summary: got %+v; want {new:0 upd:0 ev_new:0 ev_skip:12 short:true}", summary)
		}

		// Counts unchanged — idempotency invariant.
		assertRowCounts(t, ctx, pool, fixtures.ExpectedSample10Counts())

		// Short-circuit must NOT emit the warn line — the short-circuit
		// path carries the total journal count as EventsSkipped but the
		// orchestrator's guard excludes it (!ShortCircuited).
		if warnCount := countLogLevel(t, logBuf.Bytes(), "warn"); warnCount != 0 {
			t.Errorf("short-circuit warn count: got %d, want 0", warnCount)
		}
	})

	t.Run("modified_reingest_dedupes_and_warns", func(t *testing.T) {
		fixturePath := fixtures.WriteSample10Modified(t)

		var logBuf bytes.Buffer
		logger := zerolog.New(&logBuf)

		summary, err := ingest.Run(ctx, pool, logger, fixturePath)
		if err != nil {
			t.Fatalf("ingest.Run modified: %v", err)
		}
		// INC9900001's Updated advanced + 1 new journal entry: every row
		// upserts (ON CONFLICT DO UPDATE), and only the new body lands.
		if summary.TicketsNew != 0 || summary.TicketsUpdated != 10 ||
			summary.EventsNew != 1 || summary.EventsSkipped != 12 ||
			summary.ShortCircuited {
			t.Errorf("modified summary: got %+v; want {new:0 upd:10 ev_new:1 ev_skip:12 short:false}", summary)
		}

		// Ticket count unchanged (no new external IDs); events grew by 1;
		// imports grew by 1 (new hash). Clients + business_services
		// unchanged (nothing new introduced by the modified fixture).
		want := fixtures.ExpectedSample10Counts()
		want.TicketEvents++
		want.Imports++
		assertRowCounts(t, ctx, pool, want)

		// Verify the DO UPDATE path actually mutated INC9900001.updated_at
		// rather than silently reusing the pre-existing value. The fresh
		// fixture wrote updated_at = 2026-04-11 09:14:22; the modified
		// fixture advances it by 24h. If ON CONFLICT DO UPDATE were
		// downgraded to DO NOTHING, the integration test's row-count
		// assertions would still pass but this check would fail.
		assertTicketUpdatedAt(t, ctx, pool, "INC9900001",
			time.Date(2026, 4, 12, 9, 14, 22, 0, time.UTC))

		// Warn line MUST fire exactly once on this path.
		if warnCount := countLogLevel(t, logBuf.Bytes(), "warn"); warnCount != 1 {
			t.Errorf("modified-reingest warn count: got %d, want 1; log: %s", warnCount, logBuf.String())
		}
	})
}

// assertRowCounts checks the five Phase 1 schema tables against a Sample10
// expectation set. Each query uses a hardcoded SELECT per table — Postgres
// cannot parameterise table names, and composing "SELECT COUNT(*) FROM "
// with an external variable trips static analyzers as a SQL-injection
// lookalike even though the values are test-local constants. The explicit
// switch keeps both humans and linters satisfied.
func assertRowCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want fixtures.Sample10Counts) {
	t.Helper()
	checks := []struct {
		table string
		sql   string
		want  int
	}{
		{"tickets", "SELECT COUNT(*) FROM tickets", want.Tickets},
		{"ticket_events", "SELECT COUNT(*) FROM ticket_events", want.TicketEvents},
		{"imports", "SELECT COUNT(*) FROM imports", want.Imports},
		{"clients", "SELECT COUNT(*) FROM clients", want.Clients},
		{"business_services", "SELECT COUNT(*) FROM business_services", want.BusinessServices},
	}
	for _, c := range checks {
		var got int
		if err := pool.QueryRow(ctx, c.sql).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", c.table, err)
		}
		if got != c.want {
			t.Errorf("%s: got %d, want %d", c.table, got, c.want)
		}
	}
}

// assertTicketUpdatedAt reads the updated_at column for a specific
// ticket_external_id and asserts it equals the expected UTC time. Used by
// the modified-reingest scenario to verify that DO UPDATE SET actually
// persisted the new updated_at (the summary's TicketsUpdated counter
// alone cannot distinguish "row matched ON CONFLICT and re-wrote columns"
// from "row matched and DO NOTHING kept old values").
func assertTicketUpdatedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, externalID string, want time.Time) {
	t.Helper()
	var got time.Time
	err := pool.QueryRow(ctx,
		"SELECT updated_at FROM tickets WHERE ticket_external_id = $1",
		externalID,
	).Scan(&got)
	if err != nil {
		t.Fatalf("select updated_at for %s: %v", externalID, err)
	}
	if !got.Equal(want) {
		t.Errorf("%s updated_at: got %v, want %v", externalID, got, want)
	}
}

// assertEventsAscending verifies the Phase 1 plan's hard-stop requirement
// that journal entries land in the DB ordered ascending by event_ts,
// regardless of ServiceNow's newest-first source order. Checked per
// ticket so a single misordered row surfaces at the offending ticket_id.
func assertEventsAscending(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT ticket_id, event_ts
		FROM ticket_events
		ORDER BY ticket_id, id
	`)
	if err != nil {
		t.Fatalf("select event order: %v", err)
	}
	defer rows.Close()

	var lastTicket int64 = -1
	var lastTS time.Time
	for rows.Next() {
		var ticketID int64
		var eventTS time.Time
		if err := rows.Scan(&ticketID, &eventTS); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		if ticketID != lastTicket {
			lastTicket = ticketID
			lastTS = eventTS
			continue
		}
		if eventTS.Before(lastTS) {
			t.Errorf("ticket %d: event_ts not ascending (%v < %v)", ticketID, eventTS, lastTS)
		}
		lastTS = eventTS
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate events: %v", err)
	}
}

// countLogLevel parses the zerolog JSON lines in buf and counts how many
// carry a matching "level" field. Returns an error via t.Fatalf for
// malformed lines so a silent parse failure can't skew the assertion.
func countLogLevel(t *testing.T, buf []byte, wantLevel string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(strings.TrimRight(string(buf), "\n"), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Level string `json:"level"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse log line %q: %v", line, err)
		}
		if entry.Level == wantLevel {
			n++
		}
	}
	return n
}

// TestMain lets the test binary honour TESTCONTAINERS_RYUK_DISABLED=true
// when callers want to skip the reaper sidecar (some Docker-in-WSL setups
// block the privileged reaper container). No-op otherwise.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
