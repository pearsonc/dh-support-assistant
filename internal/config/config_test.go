package config

import (
	"os"
	"path/filepath"
	"testing"
)

// unsetEnv removes every DH_-prefixed env var for the duration of a test.
// t.Setenv("", "") still registers an override with koanf's env provider,
// so for a "bare defaults" test we must Unsetenv instead. Cleanup restores
// whatever was there at test entry.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		prev, had := os.LookupEnv(k)
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, prev) })
		}
		_ = os.Unsetenv(k)
	}
}

// TestLoadDefaults confirms every key's built-in value without any file
// or env override. Relied on by cmd/server when neither DH_CONFIG_FILE
// nor DH_*-prefixed env vars are set (e.g. a bare `go run ./cmd/server`).
func TestLoadDefaults(t *testing.T) {
	// Clear every DH_-prefixed var that a parent process might have set —
	// config.Load always reads env, and leaking env from the invoking shell
	// into a defaults test produces phantom failures.
	unsetEnv(t,
		"DH_LISTEN_ADDR",
		"DH_LOG_PATH",
		"DH_LOG_LEVEL",
		"DH_DB_URL",
		"DH_SCORING__WEIGHT_SEVERITY",
		"DH_SCORING__WEIGHT_AGE",
		"DH_SCORING__WEIGHT_DUE",
		"DH_STALENESS__THRESHOLD_DAYS",
	)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.Scoring.WeightSeverity != defaultScoringWeightSeverity {
		t.Errorf("WeightSeverity = %v, want %v", cfg.Scoring.WeightSeverity, defaultScoringWeightSeverity)
	}
	if cfg.Scoring.WeightAge != defaultScoringWeightAge {
		t.Errorf("WeightAge = %v, want %v", cfg.Scoring.WeightAge, defaultScoringWeightAge)
	}
	if cfg.Scoring.WeightDue != defaultScoringWeightDue {
		t.Errorf("WeightDue = %v, want %v", cfg.Scoring.WeightDue, defaultScoringWeightDue)
	}
	if cfg.Staleness.ThresholdDays != defaultStalenessThresholdDays {
		t.Errorf("ThresholdDays = %v, want %v", cfg.Staleness.ThresholdDays, defaultStalenessThresholdDays)
	}
}

// TestLoadEnvOverridesFlat verifies flat keys (single underscore) still
// map correctly — regression guard for the Phase 2.2 koanf env replacer
// change (added double-underscore-to-dot mapping).
func TestLoadEnvOverridesFlat(t *testing.T) {
	t.Setenv("DH_LISTEN_ADDR", ":9090")
	t.Setenv("DH_LOG_LEVEL", "debug")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

// TestLoadEnvOverridesNested covers the double-underscore → dot rule that
// maps DH_SCORING__WEIGHT_SEVERITY to scoring.weight_severity. Without
// this mapping nested config would be unreachable from env alone.
func TestLoadEnvOverridesNested(t *testing.T) {
	t.Setenv("DH_SCORING__WEIGHT_SEVERITY", "0.7")
	t.Setenv("DH_SCORING__WEIGHT_AGE", "0.1")
	t.Setenv("DH_SCORING__WEIGHT_DUE", "0.2")
	t.Setenv("DH_STALENESS__THRESHOLD_DAYS", "5")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Scoring.WeightSeverity != 0.7 {
		t.Errorf("WeightSeverity = %v, want 0.7", cfg.Scoring.WeightSeverity)
	}
	if cfg.Scoring.WeightAge != 0.1 {
		t.Errorf("WeightAge = %v, want 0.1", cfg.Scoring.WeightAge)
	}
	if cfg.Scoring.WeightDue != 0.2 {
		t.Errorf("WeightDue = %v, want 0.2", cfg.Scoring.WeightDue)
	}
	if cfg.Staleness.ThresholdDays != 5 {
		t.Errorf("ThresholdDays = %v, want 5", cfg.Staleness.ThresholdDays)
	}
}

// TestLoadFileOverridesDefaultsEnvOverridesFile documents the precedence
// contract (env > file > defaults).
func TestLoadFileOverridesDefaultsEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const contents = `
listen_addr: ":7070"
scoring:
  weight_severity: 0.9
  weight_age: 0.05
  weight_due: 0.05
staleness:
  threshold_days: 7
`
	if err := writeFile(path, contents); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// env trumps file.
	t.Setenv("DH_STALENESS__THRESHOLD_DAYS", "2")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":7070" {
		t.Errorf("ListenAddr = %q, want :7070 (from file)", cfg.ListenAddr)
	}
	if cfg.Scoring.WeightSeverity != 0.9 {
		t.Errorf("WeightSeverity = %v, want 0.9 (from file)", cfg.Scoring.WeightSeverity)
	}
	if cfg.Staleness.ThresholdDays != 2 {
		t.Errorf("ThresholdDays = %v, want 2 (env overrides file)", cfg.Staleness.ThresholdDays)
	}
}

// writeFile is a test helper that keeps the test file free of os/io
// boilerplate so the intent (env vs file vs default) stays front-and-centre.
func writeFile(path, body string) error {
	return osWriteFile(path, []byte(body), 0o644)
}
