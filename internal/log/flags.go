package log

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagLogLevel = flag.Flag[slog.Level]{
		Name:        "log-level",
		Description: "The log level to use. (trace, debug, info, warn, error)",
		Default:     slog.LevelInfo,
		Parse: func(s string) (slog.Level, error) {
			var logLevel slog.Level
			if strings.EqualFold(s, "trace") {
				logLevel = LevelTrace
			} else {
				err := logLevel.UnmarshalText([]byte(s))
				if err != nil {
					return logLevel, fmt.Errorf("failed to parse log level: %w", err)
				}
			}
			return logLevel, nil
		},
	}

	FlagLogFormat = flag.Flag[string]{
		Name:        "log-format",
		Description: "The log format to use. (json, text)",
		Default:     "json",
		Action: func(s string) error {
			if s != "json" && s != "text" {
				return fmt.Errorf("invalid log format: %s", s)
			}
			return nil
		},
	}
)
