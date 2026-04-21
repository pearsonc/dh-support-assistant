package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

func TestListTicketsHappyPath(t *testing.T) {
	items := []queries.TicketListItem{
		{
			ID:               1,
			TicketExternalID: "INC0000001",
			ShortDescription: "Example",
			State:            "In Progress",
			Severity:         "High",
			Priority:         "2 - High",
			AssignmentGroup:  "L2 Analytics",
			OpenedAt:         time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
			UpdatedAt:        time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
			DueDate:          time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC),
		},
	}
	handler, mock := newTestAPI(&mockQueries{
		listTicketsResult: queries.TicketListResult{Items: items, Total: 1},
	})
	w := doRequest(handler, http.MethodGet, "/api/tickets?limit=10&offset=0")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if mock.listTicketsCalls != 1 {
		t.Errorf("ListTickets calls = %d, want 1", mock.listTicketsCalls)
	}
	if mock.listTicketsParams.Limit != 10 {
		t.Errorf("Limit = %d, want 10", mock.listTicketsParams.Limit)
	}
	if mock.listTicketsParams.Sort != queries.SortUpdatedAtDesc {
		t.Errorf("Sort = %q, want default %q", mock.listTicketsParams.Sort, queries.SortUpdatedAtDesc)
	}

	var env TicketListEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Total != 1 || len(env.Data) != 1 {
		t.Errorf("envelope = %+v, want total=1 with 1 item", env)
	}
	if env.Data[0].TicketExternalID != "INC0000001" {
		t.Errorf("first item TicketExternalID = %q, want %q", env.Data[0].TicketExternalID, "INC0000001")
	}
}

func TestListTicketsFilterPassthrough(t *testing.T) {
	// Handlers must pass parsed filters into queries unaltered so future
	// changes to the filter set don't silently drop params.
	handler, mock := newTestAPI(&mockQueries{})
	_ = doRequest(handler, http.MethodGet, "/api/tickets?severity=High&state=In+Progress&sort=opened_at_desc")

	if mock.listTicketsParams.Severity != "High" {
		t.Errorf("Severity = %q, want High", mock.listTicketsParams.Severity)
	}
	if mock.listTicketsParams.State != "In Progress" {
		t.Errorf("State = %q, want \"In Progress\"", mock.listTicketsParams.State)
	}
	if mock.listTicketsParams.Sort != queries.SortOpenedAtDesc {
		t.Errorf("Sort = %q, want %q", mock.listTicketsParams.Sort, queries.SortOpenedAtDesc)
	}
}

func TestListTicketsBadParams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		want   string
	}{
		{"non-numeric limit", "/api/tickets?limit=abc", ErrInvalidParam},
		{"non-numeric offset", "/api/tickets?offset=abc", ErrInvalidParam},
		{"unknown sort", "/api/tickets?sort=magic", ErrInvalidParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, mock := newTestAPI(&mockQueries{})
			w := doRequest(handler, http.MethodGet, tc.target)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if mock.listTicketsCalls != 0 {
				t.Errorf("ListTickets called despite bad params (%d times)", mock.listTicketsCalls)
			}
			var env ErrorEnvelope
			if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if env.Error.Code != tc.want {
				t.Errorf("code = %q, want %q", env.Error.Code, tc.want)
			}
		})
	}
}

func TestListTicketsBackendError(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{listTicketsErr: errStub})
	w := doRequest(handler, http.MethodGet, "/api/tickets")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestGetTicketHappyPath(t *testing.T) {
	detail := queries.TicketDetail{
		ID:               42,
		TicketExternalID: "INC0000042",
		ShortDescription: "Test ticket",
		Description:      "Full description",
		State:            "Resolved",
		Severity:         "Moderate",
		Priority:         "3 - Moderate",
		Urgency:          "Low",
		Impact:           "Low",
		AssignmentGroup:  "L2 Analytics",
		OpenedAt:         time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
		OpenedBy:         "alice",
		UpdatedBy:        "bob",
		Caller:           "alice",
		CreatedAt:        time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC),
		CreatedBy:        "alice",
		DueDate:          time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
	}
	events := []queries.TicketEventItem{
		{
			ID:            1,
			EventTS:       time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
			AuthorName:    "bob",
			AuthorContext: "Work Notes",
			Body:          "Investigating",
		},
	}
	handler, mock := newTestAPI(&mockQueries{
		getTicketResult: queries.TicketDetailResult{Ticket: detail, Events: events},
	})
	w := doRequest(handler, http.MethodGet, "/api/tickets/42")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if mock.getTicketID != 42 {
		t.Errorf("GetTicket called with id=%d, want 42", mock.getTicketID)
	}

	var env TicketDetailEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Ticket.ID != 42 || len(env.Events) != 1 {
		t.Errorf("envelope = %+v, want id=42 with 1 event", env)
	}
}

func TestGetTicketInvalidID(t *testing.T) {
	for _, idStr := range []string{"abc", "0", "-5"} {
		t.Run(idStr, func(t *testing.T) {
			handler, mock := newTestAPI(&mockQueries{})
			w := doRequest(handler, http.MethodGet, fmt.Sprintf("/api/tickets/%s", idStr))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("id=%s: status = %d, want 400", idStr, w.Code)
			}
			if mock.getTicketCalls != 0 {
				t.Errorf("id=%s: GetTicket called despite invalid id", idStr)
			}
		})
	}
}

func TestGetTicketNotFound(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{getTicketErr: queries.ErrTicketNotFound})
	w := doRequest(handler, http.MethodGet, "/api/tickets/999")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != ErrNotFound {
		t.Errorf("code = %q, want %q", env.Error.Code, ErrNotFound)
	}
}

func TestGetTicketBackendError(t *testing.T) {
	handler, _ := newTestAPI(&mockQueries{getTicketErr: errStub})
	w := doRequest(handler, http.MethodGet, "/api/tickets/1")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
