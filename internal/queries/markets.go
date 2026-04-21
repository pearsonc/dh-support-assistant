package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// marketRollupSQL pivots business_services by country_code. Internal
// services (dunnhumby) have country_code = NULL and surface as their own
// row; the handler may re-label NULL to "internal" for display without
// changing the SQL. DISTINCT on client / ticket counts because a
// country_code may have multiple business_services per client.
const marketRollupSQL = `
SELECT
  bs.country_code,
  COUNT(DISTINCT bs.client_id) AS client_count,
  COUNT(DISTINCT bs.id)        AS business_service_count,
  COUNT(DISTINCT t.id)         AS ticket_count
FROM business_services bs
LEFT JOIN tickets t ON t.business_service_id = bs.id
GROUP BY bs.country_code
ORDER BY ticket_count DESC, bs.country_code ASC NULLS LAST
`

// ListMarkets returns one row per distinct country_code. Sort: most-
// active-first by ticket count; ties broken by country_code ASC with NULL
// sorted last so the internal-services bucket doesn't dominate the top of
// the list by accident.
func (q *Queries) ListMarkets(ctx context.Context) ([]MarketRollup, error) {
	rows, err := q.pool.Query(ctx, marketRollupSQL)
	if err != nil {
		return nil, fmt.Errorf("list markets query: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[MarketRollup])
	if err != nil {
		return nil, fmt.Errorf("list markets collect: %w", err)
	}
	return items, nil
}
