package ingest

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Run orchestrates one ingest. It reads the xlsx, normalises cells, maps
// rows to Records, computes the content hash, and calls Write which owns
// the transactional insert. The whole pipeline runs in-memory for the
// Phase 0 sample size (412 rows / ~249 KB) per Phase 1 Risk #2 which
// explicitly defers streaming until GB-scale exports appear.
//
// On a fresh run: Summary reports TicketsNew, EventsNew, ElapsedMS.
// On a re-run with an unchanged file: Summary reports all zero counts
// except EventsSkipped which carries the full journal count from the
// file, proving the writer recognised every row but deduplicated them
// all — matching the Phase 1 plan's acceptance-check JSON shape.
func Run(ctx context.Context, pool *pgxpool.Pool, logger zerolog.Logger, path string) (Summary, error) {
	file, err := Read(path)
	if err != nil {
		return Summary{}, err
	}

	cols, err := Headers(file.Headers)
	if err != nil {
		return Summary{}, err
	}

	records := make([]Record, 0, len(file.Rows))
	for i, row := range file.Rows {
		rec, err := MapRow(cols, row)
		if err != nil {
			// i is 0-indexed over data rows; +2 gives the 1-indexed
			// xlsx row number (header is row 1).
			return Summary{}, fmt.Errorf("ingest: map row %d: %w", i+2, err)
		}
		records = append(records, rec)
	}

	hashInput := make([][]string, 0, len(file.Rows)+1)
	hashInput = append(hashInput, file.Headers)
	hashInput = append(hashInput, file.Rows...)
	hash := ContentHash(hashInput)

	fileName := filepath.Base(path)
	totalEvents := countJournalEntries(records)

	logger.Info().
		Str("path", path).
		Str("file_name", fileName).
		Str("import_hash", hash).
		Int("rows", len(records)).
		Int("journal_entries", totalEvents).
		Msg("ingest: starting write")

	summary, err := Write(ctx, pool, hash, fileName, records)
	if err != nil {
		logger.Error().Err(err).Str("import_hash", hash).Msg("ingest: write failed")
		return summary, err
	}

	logger.Info().
		Str("import_hash", hash).
		Int("tickets_new", summary.TicketsNew).
		Int("tickets_updated", summary.TicketsUpdated).
		Int("events_new", summary.EventsNew).
		Int("events_skipped", summary.EventsSkipped).
		Int64("elapsed_ms", summary.ElapsedMS).
		Msg("ingest: write complete")

	return summary, nil
}
