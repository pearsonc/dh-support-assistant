package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/pearsonc/dh-support-assistant/internal/config"
	"github.com/pearsonc/dh-support-assistant/internal/db"
	"github.com/pearsonc/dh-support-assistant/internal/logging"
	"github.com/pearsonc/dh-support-assistant/internal/server"
)

const (
	dbBootTimeout  = 10 * time.Second
	migrateTimeout = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		// Pre-logger fallback: run() may return an error before the logger is
		// constructed (config load, log file open). Surface it on stderr so
		// the operator sees the bootstrap failure. Post-logger errors are
		// already logged; this fprintf is the last-resort path only.
		fmt.Fprintf(os.Stderr, "server exited: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// -migrate controls the startup behaviour. "" is normal server start
	// (migrations still run first, per the Phase 1 plan wiring); "up" and
	// "down" run migrations against DH_DB_URL and exit — how `make migrate`
	// and `make migrate-down` drive host-side rollout without starting the
	// HTTP listener.
	migrateOp := flag.String("migrate", "", "migrate up|down then exit; empty means normal server start")
	flag.Parse()

	cfg, err := config.Load(os.Getenv("DH_CONFIG_FILE"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, closer, err := logging.New(cfg.LogPath, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	defer func() { _ = closer.Close() }()

	logger.Info().
		Str("listen_addr", cfg.ListenAddr).
		Str("log_path", cfg.LogPath).
		Str("log_level", cfg.LogLevel).
		Bool("db_configured", cfg.DBURL != "").
		Str("migrate_op", *migrateOp).
		Msg("server starting")

	if cfg.DBURL == "" {
		if *migrateOp != "" {
			return fmt.Errorf("db_url required for -migrate=%s", *migrateOp)
		}
		logger.Warn().Msg("db_url not configured; readiness will report no_db")
		return serveOnly(logger, cfg, nil)
	}

	bootCtx, cancel := context.WithTimeout(context.Background(), dbBootTimeout)
	pool, err := db.NewPool(bootCtx, cfg.DBURL)
	cancel()
	if err != nil {
		logger.Error().Err(err).Msg("db pool construction failed")
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()
	logger.Info().Msg("db pool connected")

	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer migrateCancel()

	switch *migrateOp {
	case "down":
		if err := db.MigrateDown(migrateCtx, pool, logger); err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		return nil
	case "up", "":
		if err := db.MigrateUp(migrateCtx, pool, logger); err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
	default:
		return fmt.Errorf("unknown -migrate value %q (expected up|down)", *migrateOp)
	}

	if *migrateOp == "up" {
		return nil
	}

	return serveOnly(logger, cfg, pool)
}

func serveOnly(logger zerolog.Logger, cfg *config.Config, pool *pgxpool.Pool) error {
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(logger, pool),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Msg("listen failed")
			return fmt.Errorf("listen: %w", err)
		}
		return nil
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("shutdown requested")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("shutdown failed")
			return fmt.Errorf("shutdown: %w", err)
		}
		logger.Info().Msg("server stopped cleanly")
		return nil
	}
}
