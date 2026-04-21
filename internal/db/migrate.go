package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	"github.com/pearsonc/dh-support-assistant/migrations"
)

// migrationDialect targets the pgvector/pgvector:pg16 image declared in
// docker/compose.yaml. Goose's "postgres" dialect covers plain pgx over
// pgvector — the vector extension itself is Phase 4's problem.
const migrationDialect = "postgres"

// MigrateUp applies all pending goose migrations bundled into the binary
// via //go:embed. Called from cmd/server/main.go AFTER the pool Ping
// succeeds and BEFORE the router is mounted, so the server never exposes
// /readiness=200 over a schema that doesn't match the code.
//
// Logs version_before, version_after, and migrations_applied so the
// plan's hard-stop "logs migration application count" assertion is
// satisfied by a single line per boot.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, logger zerolog.Logger) error {
	return runMigrations(ctx, pool, logger, "up")
}

// MigrateDown rolls back the most recent migration. Used by the
// `make migrate-down` target during schema iteration; the containerised
// app does not call this in production.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, logger zerolog.Logger) error {
	return runMigrations(ctx, pool, logger, "down")
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, logger zerolog.Logger, op string) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(migrationDialect); err != nil {
		return fmt.Errorf("db: set dialect %q: %w", migrationDialect, err)
	}
	goose.SetLogger(&gooseZerologAdapter{logger: logger})

	before, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return fmt.Errorf("db: read version before %s: %w", op, err)
	}

	switch op {
	case "up":
		if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
			return fmt.Errorf("db: goose up: %w", err)
		}
	case "down":
		if err := goose.DownContext(ctx, sqlDB, "."); err != nil {
			return fmt.Errorf("db: goose down: %w", err)
		}
	default:
		return fmt.Errorf("db: unknown migrate op %q (expected up|down)", op)
	}

	after, err := currentVersion(ctx, sqlDB)
	if err != nil {
		return fmt.Errorf("db: read version after %s: %w", op, err)
	}

	event := logger.Info().
		Str("op", op).
		Int64("version_before", before).
		Int64("version_after", after)

	switch op {
	case "up":
		applied := 0
		if after > before {
			applied = int(after - before)
		}
		event.Int("migrations_applied", applied).Msg("migrations up complete")
	case "down":
		rolled := 0
		if before > after {
			rolled = int(before - after)
		}
		event.Int("migrations_rolled_back", rolled).Msg("migration down complete")
	}
	return nil
}

// currentVersion wraps goose.GetDBVersionContext with a simpler signature.
// Goose returns version 0 against a virgin DB (no goose_db_version table
// yet) — it creates the tracking table on first run, so the pre-up read
// against a brand-new pgvector container is expected to return 0.
func currentVersion(ctx context.Context, sqlDB *sql.DB) (int64, error) {
	v, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return 0, fmt.Errorf("goose version: %w", err)
	}
	return v, nil
}

// gooseZerologAdapter routes goose's internal log output through zerolog
// per [Rule: Log to Files]. Goose uses Printf for progress lines
// ("OK 0001_imports.sql") and Fatalf for unrecoverable conditions — the
// latter never exits the process here; it only logs at error level so the
// caller's error return drives the actual shutdown.
type gooseZerologAdapter struct {
	logger zerolog.Logger
}

func (g *gooseZerologAdapter) Fatalf(format string, v ...interface{}) {
	g.logger.Error().Msgf("goose: "+format, v...)
}

func (g *gooseZerologAdapter) Printf(format string, v ...interface{}) {
	g.logger.Info().Msgf("goose: "+format, v...)
}
