package log

import (
	"log/slog"
	"os"
)

func Init(cfg *Config) {
	logOptions := &slog.HandlerOptions{
		Level: cfg.Level.Level,
		ReplaceAttr: func(groups []string, field slog.Attr) slog.Attr {
			if field.Key == slog.LevelKey && field.Value.Any().(slog.Level) == LevelTrace {
				field.Value = slog.StringValue("TRACE")
			}
			return field
		},
	}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, logOptions)
	} else {
		handler = slog.NewTextHandler(os.Stderr, logOptions)
	}

	slog.SetDefault(slog.New(slogHandler{Handler: handler}))
}

func Handler() slog.Handler {
	return slog.Default().Handler()
}
