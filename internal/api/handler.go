// Package api exposes the read-only JSON API consumed by the Phase 2 web
// dashboard. Handlers never touch SQL directly; all data access goes through
// the Queries interface, which internal/queries.*Queries implements.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

// RequestTimeout bounds every handler's DB work. Plan 2.1 hard-stop: every
// handler wraps r.Context() with this timeout before hitting Queries.
// Overrides per-handler only if a legitimate slower query lands in Phase 2.6.
const RequestTimeout = 2 * time.Second

// Queries is the abstract store handlers depend on. queries.Queries
// satisfies it by shape; tests inject a stub so handler logic can be
// exercised without a live Postgres.
type Queries interface {
	Stats(ctx context.Context) (queries.StatsResult, error)
	ListTickets(ctx context.Context, p queries.ListTicketsParams) (queries.TicketListResult, error)
	GetTicket(ctx context.Context, id int64) (queries.TicketDetailResult, error)
	ListClients(ctx context.Context) ([]queries.ClientRollup, error)
	ListMarkets(ctx context.Context) ([]queries.MarketRollup, error)
}

// Compile-time guard: any drift in queries.Queries' signatures that breaks
// the interface fails at build, not at mount time.
var _ Queries = (*queries.Queries)(nil)

// API bundles handler dependencies. Constructed once in cmd/server and
// registered against the chi router.
type API struct {
	logger  zerolog.Logger
	queries Queries
}

// New returns an API bound to logger + queries. queries MUST NOT be nil —
// the caller in cmd/server guards this at startup.
func New(logger zerolog.Logger, q Queries) *API {
	return &API{logger: logger, queries: q}
}

// Register mounts every handler under the router passed in. Caller does
// r.Route("/api", api.Register) to keep the /api prefix owned by the
// mount site, not the handlers.
func (a *API) Register(r chi.Router) {
	r.Get("/stats", a.getStats)
	r.Get("/tickets", a.listTickets)
	r.Get("/tickets/{id}", a.getTicket)
	r.Get("/clients", a.listClients)
	r.Get("/markets", a.listMarkets)
}

// withTimeout wraps the request context with the package-level RequestTimeout.
// Callers defer the returned cancel immediately. Kept as a helper to stop
// handlers re-typing the pattern five times (and occasionally forgetting).
func withTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), RequestTimeout)
}
