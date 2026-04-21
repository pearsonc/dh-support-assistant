// Package queries owns every read-side SQL statement the HTTP API serves.
// Queries depends on pgxpool directly; callers (handlers) never see SQL.
// All queries are read-only in Phase 2 — no INSERT/UPDATE/DELETE lives
// outside internal/ingest.
package queries

import "github.com/jackc/pgx/v5/pgxpool"

// Queries is the handle API handlers call. A single instance is shared across
// handlers; its methods are safe for concurrent use because pgxpool serialises
// connection acquisition.
type Queries struct {
	pool *pgxpool.Pool
}

// New constructs a Queries bound to pool. pool MUST be non-nil; the HTTP
// layer guards against a nil pool at mount time (Register returns early
// when no DB is configured, matching the readiness no_db fallback).
func New(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}
