// Package server exposes the HTTP surface for dh-support-assistant. Phase 1.2
// adds /readiness backed by a Postgres ping — the handler returns 200 only
// when the pool exists AND the ping succeeds within a tight budget.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// readinessPingTimeout caps how long /readiness will wait on Postgres before
// reporting 503. A slow DB blocks the probe; this keeps orchestrators from
// hanging on a stuck pool.
const readinessPingTimeout = 2 * time.Second

// New builds a chi router mounting /health and /readiness. pool may be nil —
// when it is, /readiness reports {"status":"no_db"} and 503 so `make run`
// (no DB configured) still boots cleanly. Inside Docker the pool is always
// wired and /readiness flips to 200 once the DB accepts a ping.
func New(logger zerolog.Logger, pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(recoverer(logger))

	r.Get("/health", healthHandler)
	r.Get("/readiness", readinessHandler(logger, pool))

	logger.Info().Msg("router mounted: /health, /readiness")
	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readinessHandler returns 200 when pool.Ping succeeds, 503 otherwise. nil
// pool => "no_db" (misconfigured or dev-only); ping error => "db_unreachable".
func readinessHandler(logger zerolog.Logger, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "no_db"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), readinessPingTimeout)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			logger.Warn().Err(err).Msg("readiness: db ping failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unreachable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// recoverer replaces chi's stock middleware.Recoverer — which writes stacks to
// os.Stderr — with one that logs through zerolog per [Rule: Log to Files].
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
