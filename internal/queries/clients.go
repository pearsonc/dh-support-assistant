package queries

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// clientRollupSQL aggregates clients with their owned business-service count
// and live ticket count. LEFT JOINs keep a client in the result even if they
// temporarily have zero tickets. Tickets are counted DISTINCT because a
// client may own multiple business_services and each owns multiple tickets.
const clientRollupSQL = `
SELECT
  c.id,
  c.client_name,
  c.first_seen_at,
  COUNT(DISTINCT t.id)  AS ticket_count,
  COUNT(DISTINCT bs.id) AS business_service_count
FROM clients c
LEFT JOIN business_services bs ON bs.client_id = c.id
LEFT JOIN tickets t           ON t.business_service_id = bs.id
GROUP BY c.id, c.client_name, c.first_seen_at
ORDER BY ticket_count DESC, c.client_name ASC
`

// ListClients returns every client with aggregated counts. Sort is
// ticket_count DESC so the most active clients surface first; name ASC
// breaks ties deterministically.
func (q *Queries) ListClients(ctx context.Context) ([]ClientRollup, error) {
	rows, err := q.pool.Query(ctx, clientRollupSQL)
	if err != nil {
		return nil, fmt.Errorf("list clients query: %w", err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[ClientRollup])
	if err != nil {
		return nil, fmt.Errorf("list clients collect: %w", err)
	}
	return items, nil
}
