package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

func TestGetStatsHappyPath(t *testing.T) {
	want := queries.StatsResult{
		TicketsTotal:          412,
		TicketsByState:        map[string]int{"New": 12, "In Progress": 300},
		TicketsBySeverity:     map[string]int{"High": 47, "Moderate": 200},
		ClientsTotal:          12,
		BusinessServicesTotal: 61,
		LastImport: &queries.LastImport{
			ID:         1,
			FileName:   "incident.xlsx",
			RowCount:   412,
			IngestedAt: time.Date(2026, 4, 21, 10, 30, 0, 0, time.UTC),
		},
	}

	handler, mock := newTestAPI(&mockQueries{statsResult: want})
	w := doRequest(handler, http.MethodGet, "/api/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if mock.statsCalls != 1 {
		t.Errorf("Stats calls = %d, want 1", mock.statsCalls)
	}

	var got queries.StatsResult
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TicketsTotal != want.TicketsTotal {
		t.Errorf("TicketsTotal = %d, want %d", got.TicketsTotal, want.TicketsTotal)
	}
	if got.LastImport == nil || got.LastImport.RowCount != 412 {
		t.Errorf("LastImport = %+v, want RowCount=412", got.LastImport)
	}
}

func TestGetStatsEmptyImports(t *testing.T) {
	// Pre-first-ingest: last_import is null. Server must emit literal
	// JSON null so the client can render a "no data yet" placeholder.
	handler, _ := newTestAPI(&mockQueries{statsResult: queries.StatsResult{
		TicketsByState:    map[string]int{},
		TicketsBySeverity: map[string]int{},
		LastImport:        nil,
	}})
	w := doRequest(handler, http.MethodGet, "/api/stats")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw["last_import"] != nil {
		t.Errorf("last_import = %v, want null", raw["last_import"])
	}
}

func TestGetStatsBackendError(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{statsErr: errStub})
	w := doRequest(handler, http.MethodGet, "/api/stats")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != ErrInternal {
		t.Errorf("code = %q, want %q", env.Error.Code, ErrInternal)
	}
}
