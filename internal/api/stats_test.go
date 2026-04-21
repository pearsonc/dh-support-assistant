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
		PriorityBuckets: map[string]int{
			"critical": 5, "high": 12, "moderate": 200, "low": 100, "planning": 95,
		},
		StaleCount:         47,
		WeightsInUse:       queries.StatsWeights{Severity: 0.5, Age: 0.2, Due: 0.3},
		StaleThresholdDays: 3,
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
	if got.StaleCount != 47 {
		t.Errorf("StaleCount = %d, want 47", got.StaleCount)
	}
	if got.StaleThresholdDays != 3 {
		t.Errorf("StaleThresholdDays = %d, want 3", got.StaleThresholdDays)
	}
	if got.WeightsInUse.Severity != 0.5 || got.WeightsInUse.Age != 0.2 || got.WeightsInUse.Due != 0.3 {
		t.Errorf("WeightsInUse = %+v, want {0.5, 0.2, 0.3}", got.WeightsInUse)
	}
	if got.PriorityBuckets["critical"] != 5 || got.PriorityBuckets["moderate"] != 200 {
		t.Errorf("PriorityBuckets = %v, want {critical: 5, moderate: 200}", got.PriorityBuckets)
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
