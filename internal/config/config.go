// Package config loads runtime configuration from defaults, an optional
// config.yaml file, and DH_-prefixed environment variables (in that order of
// precedence: env > file > defaults).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	KeyListenAddr             = "listen_addr"
	KeyLogPath                = "log_path"
	KeyLogLevel               = "log_level"
	KeyDBURL                  = "db_url"
	KeyScoringWeightSeverity  = "scoring.weight_severity"
	KeyScoringWeightAge       = "scoring.weight_age"
	KeyScoringWeightDue       = "scoring.weight_due"
	KeyStalenessThresholdDays = "staleness.threshold_days"
)

const (
	defaultListenAddr             = ":8080"
	defaultLogPath                = "logs/app.log"
	defaultLogLevel               = "info"
	defaultDBURL                  = ""
	defaultScoringWeightSeverity  = 0.5
	defaultScoringWeightAge       = 0.2
	defaultScoringWeightDue       = 0.3
	defaultStalenessThresholdDays = 3
)

const envPrefix = "DH_"

type Config struct {
	ListenAddr string    `koanf:"listen_addr"`
	LogPath    string    `koanf:"log_path"`
	LogLevel   string    `koanf:"log_level"`
	DBURL      string    `koanf:"db_url"`
	Scoring    Scoring   `koanf:"scoring"`
	Staleness  Staleness `koanf:"staleness"`
}

// Scoring carries the tunable weights the priority scorer applies per
// plan Decision 3. Defaults sum to 1.0 so the resulting score stays in
// [0, 1]; overrides that break the sum produce a proportionally scaled
// score — documented, not defended.
type Scoring struct {
	WeightSeverity float64 `koanf:"weight_severity"`
	WeightAge      float64 `koanf:"weight_age"`
	WeightDue      float64 `koanf:"weight_due"`
}

// Staleness carries the heuristic threshold used by StaleAsOf. Expressed
// in days because that matches how the user triages ("older than three
// days" is the natural unit); scorer converts to time.Duration at the
// call site.
type Staleness struct {
	ThresholdDays int `koanf:"threshold_days"`
}

// Load resolves configuration. configPath may be empty; a non-existent file at
// configPath is treated as absent (not an error). Callers typically pass
// os.Getenv("DH_CONFIG_FILE") or a known default.
//
// Nested keys (e.g. scoring.weight_severity) are overridable from env via
// double-underscore: DH_SCORING__WEIGHT_SEVERITY → scoring.weight_severity.
// Single-underscore env vars keep mapping to flat keys unchanged, so
// DH_LISTEN_ADDR → listen_addr still works.
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(map[string]any{
		KeyListenAddr:             defaultListenAddr,
		KeyLogPath:                defaultLogPath,
		KeyLogLevel:               defaultLogLevel,
		KeyDBURL:                  defaultDBURL,
		KeyScoringWeightSeverity:  defaultScoringWeightSeverity,
		KeyScoringWeightAge:       defaultScoringWeightAge,
		KeyScoringWeightDue:       defaultScoringWeightDue,
		KeyStalenessThresholdDays: defaultStalenessThresholdDays,
	}, "."), nil); err != nil {
		return nil, fmt.Errorf("config: load defaults: %w", err)
	}

	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
				return nil, fmt.Errorf("config: load file %s: %w", configPath, err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("config: stat %s: %w", configPath, err)
		}
	}

	if err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		trimmed := strings.ToLower(strings.TrimPrefix(s, envPrefix))
		return strings.ReplaceAll(trimmed, "__", ".")
	}), nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return &cfg, nil
}
