package log

import (
	"fmt"
	"log/slog"
	"strings"
)

// Config holds the log configuration.
type Config struct {
	Level  LogLevel `flag:"log-level" description:"The log level to use. (trace, debug, info, warn, error)" default:"info"`
	Format string   `flag:"log-format" description:"The log format to use. (json, text)" default:"json"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Format != "json" && c.Format != "text" {
		return fmt.Errorf("invalid log format: %s", c.Format)
	}
	return nil
}

// LogLevel wraps slog.Level with text unmarshaling support.
type LogLevel struct {
	slog.Level
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (l *LogLevel) UnmarshalText(text []byte) error {
	s := string(text)
	if strings.EqualFold(s, "trace") {
		l.Level = LevelTrace
		return nil
	}
	return l.Level.UnmarshalText(text)
}
