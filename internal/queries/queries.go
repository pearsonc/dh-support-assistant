// Package queries owns every read-side SQL statement the HTTP API serves.
// Queries depends on pgxpool directly; callers (handlers) never see SQL.
// All queries are read-only in Phase 2 — no INSERT/UPDATE/DELETE lives
// outside internal/ingest.
package queries

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pearsonc/dh-support-assistant/internal/scoring"
)

// Queries is the handle API handlers call. A single instance is shared across
// handlers; its methods are safe for concurrent use because pgxpool serialises
// connection acquisition. Phase 2.2 adds a Scorer and staleness threshold so
// list/detail/stats can annotate every ticket with a priority score and a
// stale flag without each handler re-deriving either.
type Queries struct {
	pool              *pgxpool.Pool
	scorer            *scoring.Scorer
	stalenessDuration time.Duration
	stalenessDays     int
}

// New constructs a Queries bound to pool + scorer. pool MUST be non-nil;
// the HTTP layer guards against a nil pool at mount time (Register returns
// early when no DB is configured, matching the readiness no_db fallback).
// scorer MUST be non-nil; a no-op zerolog scorer is acceptable in tests.
// stalenessDays is the Plan §Decision-4 threshold in days.
func New(pool *pgxpool.Pool, scorer *scoring.Scorer, stalenessDays int) *Queries {
	return &Queries{
		pool:              pool,
		scorer:            scorer,
		stalenessDuration: time.Duration(stalenessDays) * 24 * time.Hour,
		stalenessDays:     stalenessDays,
	}
}

// Scorer exposes the underlying scorer. Handlers surface the weights on
// /api/stats so the dashboard can display the ordering rationale; the
// scorer is the single authoritative source of those weights.
func (q *Queries) Scorer() *scoring.Scorer { return q.scorer }

// StalenessDays returns the configured threshold in whole days for
// /api/stats transparency.
func (q *Queries) StalenessDays() int { return q.stalenessDays }
