package api

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

// Error codes returned in the JSON envelope. Stable strings — web client
// switches on these, not the HTTP status alone.
const (
	ErrBadRequest   = "bad_request"
	ErrNotFound     = "not_found"
	ErrInternal     = "internal"
	ErrUnknownSort  = "unknown_sort"
	ErrInvalidID    = "invalid_id"
	ErrInvalidParam = "invalid_parameter"
)

// ErrorBody is the inner shape of an error envelope.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorEnvelope is what clients parse on non-2xx responses. Format matches
// the project's JSON response convention (see plan §Domain: Go).
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// writeJSON encodes body as JSON and sets status. Kept private to internal/api
// so the router.go and api packages don't have to agree on a shared helper —
// each package owns its tiny copy.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError standardises non-2xx responses. Logs at Warn (not Error) for
// 4xx so client misuse doesn't pollute the error dashboard; 5xx logs at
// Error so alerts fire.
func writeError(w http.ResponseWriter, logger zerolog.Logger, status int, code, message string) {
	event := logger.Warn()
	if status >= 500 {
		event = logger.Error()
	}
	// Field name is "reason" (not "message") to avoid colliding with
	// zerolog's Msg output key, which is also "message". Duplicate keys in
	// a single JSON line are ambiguous for downstream log parsers.
	event.
		Int("status", status).
		Str("code", code).
		Str("reason", message).
		Msg("api error")
	writeJSON(w, status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}
