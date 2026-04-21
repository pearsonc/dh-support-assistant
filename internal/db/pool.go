// Package db owns the Postgres connection pool. Phase 1 plan pre-flight
// mandates pgx/v5 + pgxpool — not database/sql wrapping lib/pq.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultPingTimeout bounds the startup ping. A slow DB is a failing DB at
// boot — the app is useless without the pool so we refuse to start.
const DefaultPingTimeout = 5 * time.Second

// NewPool builds a pgxpool from dbURL and verifies reachability with Ping.
// The caller MUST call Close on the returned pool at shutdown. If Ping
// fails the pool is closed before returning so no sockets leak.
func NewPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, DefaultPingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: initial ping: %w", err)
	}
	return pool, nil
}
