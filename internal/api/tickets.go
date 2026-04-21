package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/pearsonc/dh-support-assistant/internal/queries"
)

// listTickets serves GET /api/tickets. Query params: limit, offset, sort,
// severity, state. Unknown sort tokens → 400 bad_request (not silent
// fallback) so API clients catch typos during development.
func (a *API) listTickets(w http.ResponseWriter, r *http.Request) {
	params, err := parseListTicketsParams(r)
	if err != nil {
		writeError(w, a.logger, http.StatusBadRequest, ErrInvalidParam, err.Error())
		return
	}

	ctx, cancel := withTimeout(r)
	defer cancel()

	result, err := a.queries.ListTickets(ctx, params)
	if err != nil {
		if errors.Is(err, queries.ErrUnknownSort) {
			writeError(w, a.logger, http.StatusBadRequest, ErrUnknownSort, err.Error())
			return
		}
		writeError(w, a.logger, http.StatusInternalServerError, ErrInternal, "failed to list tickets")
		a.logger.Error().Err(err).Msg("tickets list: query failed")
		return
	}

	writeJSON(w, http.StatusOK, TicketListEnvelope{
		Data: result.Items,
		Meta: PageMeta{Total: result.Total, Limit: params.Limit, Offset: params.Offset},
	})
}

// getTicket serves GET /api/tickets/{id}. Non-numeric id → 400; unknown
// id → 404. Both map through the same structured error envelope.
func (a *API) getTicket(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, a.logger, http.StatusBadRequest, ErrInvalidID, "ticket id must be a positive integer")
		return
	}

	ctx, cancel := withTimeout(r)
	defer cancel()

	result, err := a.queries.GetTicket(ctx, id)
	if err != nil {
		if errors.Is(err, queries.ErrTicketNotFound) {
			writeError(w, a.logger, http.StatusNotFound, ErrNotFound, "ticket not found")
			return
		}
		writeError(w, a.logger, http.StatusInternalServerError, ErrInternal, "failed to load ticket")
		a.logger.Error().Err(err).Int64("ticket_id", id).Msg("ticket detail: query failed")
		return
	}

	writeJSON(w, http.StatusOK, TicketDetailEnvelope{
		Ticket: result.Ticket,
		Events: result.Events,
	})
}

// parseListTicketsParams parses + validates the query string into a
// Normalise'd ListTicketsParams. Returns a descriptive error for the 400
// envelope when a numeric param is malformed; unknown sort tokens fall
// through to the queries layer's ErrUnknownSort so the tokenset stays
// single-sourced there.
func parseListTicketsParams(r *http.Request) (queries.ListTicketsParams, error) {
	q := r.URL.Query()
	p := queries.ListTicketsParams{
		Sort:     q.Get("sort"),
		Severity: q.Get("severity"),
		State:    q.Get("state"),
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return p, errors.New("limit must be an integer")
		}
		p.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return p, errors.New("offset must be an integer")
		}
		p.Offset = n
	}

	if err := p.Normalise(); err != nil {
		return p, err
	}
	return p, nil
}
