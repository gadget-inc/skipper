package log

import (
	"fmt"
	"log/slog"
	"strings"
)

// Config holds the log configuration.
type Config struct {
	Level         LogLevel `flag:"log-level" description:"The log level to use. (trace, debug, info, warn, error)" default:"info"`
	Format        string   `flag:"log-format" description:"The log format to use. (json, text)" default:"json"`
	LogFile       string   `flag:"log-file" description:"Path to a log file. When set, logs are written to both stderr and this file."`
	LogFileLevel  string   `flag:"log-file-level" description:"Log level for the file output. Defaults to --log-level. (trace, debug, info, warn, error)"`
	LogFileFormat string   `flag:"log-file-format" description:"Log format for the file output. Defaults to --log-format. (json, text)"`
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Format != "json" && c.Format != "text" {
		return fmt.Errorf("invalid log format: %s", c.Format)
	}
	if c.LogFile != "" && c.LogFileFormat != "" && c.LogFileFormat != "json" && c.LogFileFormat != "text" {
		return fmt.Errorf("invalid log file format: %s", c.LogFileFormat)
	}
	if c.LogFile != "" && c.LogFileLevel != "" {
		if _, err := parseLevel(c.LogFileLevel); err != nil {
			return fmt.Errorf("invalid log file level: %s", c.LogFileLevel)
		}
	}
	return nil
}

func parseLevel(s string) (slog.Level, error) {
	if strings.EqualFold(s, "trace") {
		return LevelTrace, nil
	}
	var l slog.Level
	err := l.UnmarshalText([]byte(s))
	return l, err
}

// LogLevel wraps slog.Level with text unmarshaling support.
type LogLevel struct {
	slog.Level
}

// MarshalText implements encoding.TextMarshaler.
func (l LogLevel) MarshalText() ([]byte, error) {
	if l.Level == LevelTrace {
		return []byte("TRACE"), nil
	}
	return l.Level.MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (l *LogLevel) UnmarshalText(text []byte) error {
	level, err := parseLevel(string(text))
	if err != nil {
		return err
	}
	l.Level = level
	return nil
}
