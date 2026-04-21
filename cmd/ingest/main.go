// cmd/ingest runs the xlsx → Postgres ingest pipeline as a one-shot CLI.
// Its contract with `make ingest FILE=...` is: exit 0 on success with a
// single JSON summary line on stdout matching the Phase 1 plan's
// acceptance shape (tickets_new, tickets_updated, events_new,
// events_skipped, elapsed_ms); exit non-zero on any error with a short
// diagnostic on stderr. Operational detail goes to logs/ingest.log via
// zerolog per [Rule: Log to Files].
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/pearsonc/dh-support-assistant/internal/config"
	"github.com/pearsonc/dh-support-assistant/internal/db"
	"github.com/pearsonc/dh-support-assistant/internal/ingest"
	"github.com/pearsonc/dh-support-assistant/internal/logging"
)

// ingestLogPath is the ingest CLI's zerolog destination. Distinct from the
// server's logs/app.log so simultaneous host-side and container-side log
// tails do not interleave.
const ingestLogPath = "logs/ingest.log"

// ingestTimeout bounds the whole pipeline (read → hash → transactional
// write). Phase 0 sample completes in well under a second; the 5-minute
// ceiling exists for headroom on future larger exports, not as the target.
const ingestTimeout = 5 * time.Minute

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ingest: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	file := flag.String("file", "", "path to ServiceNow export (.xlsx)")
	flag.Parse()

	if *file == "" {
		return errors.New("usage: ingest -file=path/to/export.xlsx")
	}

	cfg, err := config.Load(os.Getenv("DH_CONFIG_FILE"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.DBURL == "" {
		return errors.New("DH_DB_URL is required")
	}

	logger, closer, err := logging.New(ingestLogPath, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	defer func() { _ = closer.Close() }()

	logger.Info().Str("file", *file).Msg("ingest: starting")

	ctx, cancel := context.WithTimeout(context.Background(), ingestTimeout)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	summary, err := ingest.Run(ctx, pool, logger, *file)
	if err != nil {
		return fmt.Errorf("ingest run: %w", err)
	}

	out, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	// Fprintln-to-stdout keeps the machine-readable summary on the CLI's
	// documented contract channel (stdout is the data channel; logs are
	// the narrative channel). Per [Rule: Log to Files] the banned console
	// logger forms never appear in this tree — see the project-level
	// grep check in the Phase 1 plan hard-stop.
	fmt.Fprintln(os.Stdout, string(out))
	return nil
}
