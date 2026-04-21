package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

func TestListClientsHappyPath(t *testing.T) {
	rows := []queries.ClientRollup{
		{ID: 1, ClientName: "ClientA", FirstSeenAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), TicketCount: 10, BusinessServiceCount: 3},
		{ID: 2, ClientName: "ClientB", FirstSeenAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), TicketCount: 5, BusinessServiceCount: 1},
	}
	handler, mock := newTestAPI(&mockQueries{listClientsResult: rows})
	w := doRequest(handler, http.MethodGet, "/api/clients")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if mock.listClientsCalls != 1 {
		t.Errorf("ListClients calls = %d, want 1", mock.listClientsCalls)
	}

	var env ClientListEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(env.Data))
	}
}

func TestListClientsBackendError(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{listClientsErr: errStub})
	w := doRequest(handler, http.MethodGet, "/api/clients")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestListMarketsHappyPath(t *testing.T) {
	us := "US"
	uk := "UK"
	rows := []queries.MarketRollup{
		{CountryCode: &us, ClientCount: 3, BusinessServiceCount: 8, TicketCount: 42},
		{CountryCode: &uk, ClientCount: 2, BusinessServiceCount: 5, TicketCount: 20},
		{CountryCode: nil, ClientCount: 0, BusinessServiceCount: 2, TicketCount: 6}, // internal
	}
	handler, _ := newTestAPI(&mockQueries{listMarketsResult: rows})
	w := doRequest(handler, http.MethodGet, "/api/markets")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Decode as map to assert the null-country row serialises to JSON null
	// (rather than being silently dropped by a *string bug).
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Data) != 3 {
		t.Fatalf("len(Data) = %d, want 3", len(raw.Data))
	}
	if raw.Data[2]["country_code"] != nil {
		t.Errorf("internal row country_code = %v, want null", raw.Data[2]["country_code"])
	}
}

func TestListMarketsBackendError(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{listMarketsErr: errStub})
	w := doRequest(handler, http.MethodGet, "/api/markets")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
