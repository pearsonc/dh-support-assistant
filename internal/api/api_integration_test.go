//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/pearsonc/dh-support-assistant/internal/api"
	"github.com/pearsonc/dh-support-assistant/internal/db"
	"github.com/pearsonc/dh-support-assistant/internal/ingest"
	"github.com/pearsonc/dh-support-assistant/internal/ingest/fixtures"
	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

// pgvectorImage mirrors the pin used in docker/compose.yaml and the Phase 1
// integration test. If one drifts they all must drift together — the shared
// constant is duplicated (not imported) to keep each test hermetic.
const pgvectorImage = "pgvector/pgvector:pg16@sha256:7d400e340efb42f4d8c9c12c6427adb253f726881a9985d2a471bf0eed824dff"

// TestAPIIntegration exercises the real api + queries stack against a fresh
// testcontainer Postgres populated with the Phase 1 sample-10 fixture. Each
// subtest hits one endpoint via httptest.NewServer. The assertions are
// intentionally coarse on absolute values (counts, sizes) because the
// authoritative reference is fixtures.ExpectedSample10Counts; asserting on
// shape (non-empty envelopes, correct envelope keys, correct types) is
// what proves the wiring between api.Handler, queries.Queries, and the
// real DB schema.
func TestAPIIntegration(t *testing.T) {
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

	quiet := zerolog.New(io.Discard)
	if err := db.MigrateUp(ctx, pool, quiet); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// Populate via the real ingest pipeline so the API reads exactly the
	// schema production writes. Any drift between ingest's writer and the
	// api's reader surfaces here as a failed assertion.
	fixturePath := fixtures.WriteSample10(t)
	if _, err := ingest.Run(ctx, pool, quiet, fixturePath); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Mount the production router layout: /api/* delegates into api.API.
	r := chi.NewRouter()
	a := api.New(quiet, queries.New(pool))
	r.Route("/api", a.Register)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	want := fixtures.ExpectedSample10Counts()

	t.Run("stats", func(t *testing.T) {
		var stats queries.StatsResult
		hitJSON(t, srv.URL+"/api/stats", &stats)

		if stats.TicketsTotal != want.Tickets {
			t.Errorf("tickets_total = %d, want %d", stats.TicketsTotal, want.Tickets)
		}
		if stats.ClientsTotal != want.Clients {
			t.Errorf("clients_total = %d, want %d", stats.ClientsTotal, want.Clients)
		}
		if stats.BusinessServicesTotal != want.BusinessServices {
			t.Errorf("business_services_total = %d, want %d", stats.BusinessServicesTotal, want.BusinessServices)
		}
		if stats.LastImport == nil {
			t.Fatalf("last_import nil; want populated after ingest")
		}
		if stats.LastImport.RowCount != want.Tickets {
			t.Errorf("last_import.row_count = %d, want %d", stats.LastImport.RowCount, want.Tickets)
		}
	})

	t.Run("list_tickets_default_sort", func(t *testing.T) {
		var env api.TicketListEnvelope
		hitJSON(t, srv.URL+"/api/tickets", &env)

		if env.Meta.Total != want.Tickets {
			t.Errorf("meta.total = %d, want %d", env.Meta.Total, want.Tickets)
		}
		if len(env.Data) != want.Tickets {
			t.Errorf("len(data) = %d, want %d", len(env.Data), want.Tickets)
		}
		// Default sort is updated_at DESC — verify it's monotonic.
		for i := 1; i < len(env.Data); i++ {
			if env.Data[i-1].UpdatedAt.Before(env.Data[i].UpdatedAt) {
				t.Errorf("default sort broke at i=%d: %v before %v", i, env.Data[i-1].UpdatedAt, env.Data[i].UpdatedAt)
				break
			}
		}
	})

	t.Run("list_tickets_pagination", func(t *testing.T) {
		var env api.TicketListEnvelope
		hitJSON(t, srv.URL+"/api/tickets?limit=3&offset=0", &env)

		if env.Meta.Total != want.Tickets {
			t.Errorf("meta.total = %d, want %d (total should not page-shrink)", env.Meta.Total, want.Tickets)
		}
		if env.Meta.Limit != 3 || env.Meta.Offset != 0 {
			t.Errorf("meta = %+v, want limit=3 offset=0", env.Meta)
		}
		if len(env.Data) != 3 {
			t.Errorf("len(data) = %d, want 3", len(env.Data))
		}
	})

	t.Run("list_tickets_bad_sort_returns_400", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/tickets?sort=nope")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("get_ticket_happy", func(t *testing.T) {
		// Fetch the list first to discover a valid id, then hit detail.
		var list api.TicketListEnvelope
		hitJSON(t, srv.URL+"/api/tickets?limit=1", &list)
		if len(list.Data) == 0 {
			t.Fatal("list is empty; cannot test detail")
		}
		id := list.Data[0].ID

		var detail api.TicketDetailEnvelope
		hitJSON(t, srv.URL+"/api/tickets/"+itoa(id), &detail)

		if detail.Ticket.ID != id {
			t.Errorf("detail.ticket.id = %d, want %d", detail.Ticket.ID, id)
		}
		if detail.Ticket.TicketExternalID != list.Data[0].TicketExternalID {
			t.Errorf("external_id mismatch: detail=%q list=%q", detail.Ticket.TicketExternalID, list.Data[0].TicketExternalID)
		}
		// INC9900001 carries one journal entry in the fixture; at minimum
		// the detail endpoint must not silently drop events. Assert >= 0
		// rather than exact — another ticket might be returned first.
		if detail.Events == nil {
			t.Error("events nil; want slice (possibly empty)")
		}
	})

	t.Run("get_ticket_404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/tickets/99999")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("list_clients", func(t *testing.T) {
		var env api.ClientListEnvelope
		hitJSON(t, srv.URL+"/api/clients", &env)

		if len(env.Data) != want.Clients {
			t.Errorf("len(data) = %d, want %d", len(env.Data), want.Clients)
		}
		// Every client must have a positive ticket_count because the
		// fixture guarantees every client has at least one linked ticket.
		for _, c := range env.Data {
			if c.TicketCount == 0 {
				t.Errorf("client %q has ticket_count=0", c.ClientName)
			}
		}
	})

	t.Run("list_markets", func(t *testing.T) {
		var env api.MarketListEnvelope
		hitJSON(t, srv.URL+"/api/markets", &env)

		if len(env.Data) == 0 {
			t.Fatal("no market rows returned")
		}
		var totalTickets int
		for _, m := range env.Data {
			totalTickets += m.TicketCount
		}
		// Every ticket is linked to exactly one business_service → exactly
		// one market row, so the sum across markets must equal the global
		// ticket count.
		if totalTickets != want.Tickets {
			t.Errorf("sum(ticket_count) across markets = %d, want %d", totalTickets, want.Tickets)
		}
	})
}

// hitJSON performs a GET and JSON-decodes into out. Fails the test on any
// non-200 response or decode error.
func hitJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status=%d body=%s", url, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
}

// itoa avoids the strconv import for this one integer→path use-case; keeps
// the test file's import list focused on testing concerns.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
