package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

// mockQueries is a stub Queries implementation for handler tests. Each
// method returns the configured Result OR the configured Err; Err takes
// priority so tests don't have to zero-out Result to assert an error path.
// Capturing the input on each call lets tests verify handlers pass
// parsed params to the store unaltered.
type mockQueries struct {
	// StatsResult + StatsErr
	statsResult queries.StatsResult
	statsErr    error
	statsCalls  int

	// ListTickets
	listTicketsResult queries.TicketListResult
	listTicketsErr    error
	listTicketsParams queries.ListTicketsParams
	listTicketsCalls  int

	// GetTicket
	getTicketResult queries.TicketDetailResult
	getTicketErr    error
	getTicketID     int64
	getTicketCalls  int

	// ListClients
	listClientsResult []queries.ClientRollup
	listClientsErr    error
	listClientsCalls  int

	// ListMarkets
	listMarketsResult []queries.MarketRollup
	listMarketsErr    error
	listMarketsCalls  int
}

func (m *mockQueries) Stats(_ context.Context) (queries.StatsResult, error) {
	m.statsCalls++
	return m.statsResult, m.statsErr
}

func (m *mockQueries) ListTickets(_ context.Context, p queries.ListTicketsParams) (queries.TicketListResult, error) {
	m.listTicketsCalls++
	m.listTicketsParams = p
	return m.listTicketsResult, m.listTicketsErr
}

func (m *mockQueries) GetTicket(_ context.Context, id int64) (queries.TicketDetailResult, error) {
	m.getTicketCalls++
	m.getTicketID = id
	return m.getTicketResult, m.getTicketErr
}

func (m *mockQueries) ListClients(_ context.Context) ([]queries.ClientRollup, error) {
	m.listClientsCalls++
	return m.listClientsResult, m.listClientsErr
}

func (m *mockQueries) ListMarkets(_ context.Context) ([]queries.MarketRollup, error) {
	m.listMarketsCalls++
	return m.listMarketsResult, m.listMarketsErr
}

// Compile-time assertion: mockQueries satisfies the Queries interface.
// Drift here fails tests at build time instead of at runtime.
var _ Queries = (*mockQueries)(nil)

// newTestAPI builds a silent-logger API with the supplied mock, mounted on
// a chi router with the real /api prefix so tests hit the same paths as
// production. Returns the handler and the mock so assertions can inspect
// call counts / captured params.
func newTestAPI(mock *mockQueries) (http.Handler, *mockQueries) {
	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	a := New(logger, mock)
	r := chi.NewRouter()
	r.Route("/api", a.Register)
	return r, mock
}

// doRequest is a one-liner for send-and-decode tests.
func doRequest(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// errStub is a shared sentinel error injected into mock Err fields to
// verify 5xx wrapping. Using a distinct type (rather than errors.New on
// every use) keeps test logs comparable.
var errStub = errors.New("stub backend failure")
