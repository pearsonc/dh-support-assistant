// Package server exposes the HTTP surface for dh-support-assistant. Phase 1.1
// ships /health (liveness) and /readiness (returns 503 until sub-phase 1.2
// wires a Postgres ping).
package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// New builds a chi router mounting /health and /readiness. The recoverer
// routes panics through logger rather than chi's default stderr writer, per
// [Rule: Log to Files].
func New(logger zerolog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(recoverer(logger))

	r.Get("/health", healthHandler)
	r.Get("/readiness", readinessHandler)

	logger.Info().Msg("router mounted: /health, /readiness")
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readinessHandler returns 503 until sub-phase 1.2 replaces this with a
// handler that checks the Postgres pool.
func readinessHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// recoverer replaces chi's stock middleware.Recoverer — which writes stacks to
// os.Stderr — with one that logs through zerolog.
func recoverer(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rvr := recover()
				if rvr == nil {
					return
				}
				if rvr == http.ErrAbortHandler {
					panic(rvr)
				}
				logger.Error().
					Interface("panic", rvr).
					Bytes("stack", debug.Stack()).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Str("request_id", middleware.GetReqID(r.Context())).
					Msg("panic recovered")
				w.WriteHeader(http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
