package queries

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Stats runs six small aggregations sequentially. For 412 tickets + 61
// business_services + 12 clients this completes in well under the 50ms
// plan budget; revisit if profiling shows otherwise on a larger dataset.
// Each sub-query has its own error wrapping so production logs pinpoint
// which aggregate failed.
func (q *Queries) Stats(ctx context.Context) (StatsResult, error) {
	var res StatsResult

	if err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tickets`).Scan(&res.TicketsTotal); err != nil {
		return StatsResult{}, fmt.Errorf("stats tickets total: %w", err)
	}

	byState, err := countByGroup(ctx, q.pool, `SELECT state, COUNT(*) FROM tickets GROUP BY state`)
	if err != nil {
		return StatsResult{}, fmt.Errorf("stats by_state: %w", err)
	}
	res.TicketsByState = byState

	bySeverity, err := countByGroup(ctx, q.pool, `SELECT severity, COUNT(*) FROM tickets GROUP BY severity`)
	if err != nil {
		return StatsResult{}, fmt.Errorf("stats by_severity: %w", err)
	}
	res.TicketsBySeverity = bySeverity

	if err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM clients`).Scan(&res.ClientsTotal); err != nil {
		return StatsResult{}, fmt.Errorf("stats clients total: %w", err)
	}

	if err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM business_services`).Scan(&res.BusinessServicesTotal); err != nil {
		return StatsResult{}, fmt.Errorf("stats business_services total: %w", err)
	}

	lastImport, err := latestImport(ctx, q.pool)
	if err != nil {
		return StatsResult{}, fmt.Errorf("stats last_import: %w", err)
	}
	res.LastImport = lastImport

	return res, nil
}

// countByGroup runs a `SELECT key, count FROM ... GROUP BY key` shape and
// returns a ready-to-marshal map. Values are int-cast from the pgx BIGINT
// return; at the project's expected row volumes this fits comfortably.
func countByGroup(ctx context.Context, pool pgxQuerier, sql string) (map[string]int, error) {
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out[key] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return out, nil
}

// latestImport returns the most recent imports row, or nil if the table is
// empty (pre-first-ingest). A nil pointer serialises to JSON null which is
// what the UI expects for the "no data yet" state.
func latestImport(ctx context.Context, pool pgxQuerier) (*LastImport, error) {
	rows, err := pool.Query(ctx, `
SELECT id, file_name, row_count, ingested_at
FROM imports
ORDER BY ingested_at DESC, id DESC
LIMIT 1
`)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	item, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[LastImport])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("collect: %w", err)
	}
	return &item, nil
}

// pgxQuerier is the subset of *pgxpool.Pool stats.go uses. Factored out so
// helpers don't force the whole package to import pgxpool types at call
// sites they don't need. pgxpool.Pool satisfies this implicitly.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
