// Package logging constructs the application's zerolog logger. All runtime
// logging in dh-support-assistant writes JSON lines to a file path supplied by
// [internal/config] per [Rule: Log to Files].
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

// New opens (or creates) the log file at path, ensures its parent directory
// exists, and returns a configured zerolog.Logger plus an io.Closer that the
// caller MUST close at shutdown. level is parsed as a zerolog level name; an
// unknown value is an error.
func New(path, level string) (zerolog.Logger, io.Closer, error) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.Nop(), nil, fmt.Errorf("logging: parse level %q: %w", level, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return zerolog.Nop(), nil, fmt.Errorf("logging: create log dir for %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return zerolog.Nop(), nil, fmt.Errorf("logging: open %s: %w", path, err)
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := zerolog.New(f).Level(lvl).With().Timestamp().Logger()
	return logger, f, nil
}
