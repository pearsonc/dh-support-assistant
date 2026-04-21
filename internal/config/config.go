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
	KeyListenAddr = "listen_addr"
	KeyLogPath    = "log_path"
	KeyLogLevel   = "log_level"
)

const (
	defaultListenAddr = ":8080"
	defaultLogPath    = "logs/app.log"
	defaultLogLevel   = "info"
)

const envPrefix = "DH_"

type Config struct {
	ListenAddr string `koanf:"listen_addr"`
	LogPath    string `koanf:"log_path"`
	LogLevel   string `koanf:"log_level"`
}

// Load resolves configuration. configPath may be empty; a non-existent file at
// configPath is treated as absent (not an error). Callers typically pass
// os.Getenv("DH_CONFIG_FILE") or a known default.
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(map[string]any{
		KeyListenAddr: defaultListenAddr,
		KeyLogPath:    defaultLogPath,
		KeyLogLevel:   defaultLogLevel,
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
		return strings.ToLower(strings.TrimPrefix(s, envPrefix))
	}), nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return &cfg, nil
}
