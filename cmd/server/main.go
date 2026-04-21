package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pearsonc/dh-support-assistant/internal/config"
	"github.com/pearsonc/dh-support-assistant/internal/logging"
	"github.com/pearsonc/dh-support-assistant/internal/server"
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
		Msg("server starting")

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.New(logger),
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
