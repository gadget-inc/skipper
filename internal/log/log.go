package log

import (
	"log/slog"
	"os"
	"path/filepath"
)

// Init configures the default slog logger from cfg and returns a cleanup
// function that closes any opened log file. The caller should defer the
// returned function.
func Init(cfg *Config) func() {
	handler, cleanup := buildHandler(cfg)
	slog.SetDefault(slog.New(slogHandler{Handler: handler}))
	return cleanup
}

func Handler() slog.Handler {
	return slog.Default().Handler()
}

func buildHandler(cfg *Config) (slog.Handler, func()) {
	handler := newStderrHandler(cfg)
	noop := func() {}
	if cfg.LogFile == "" {
		return handler, noop
	}
	fileHandler, cleanup, err := newFileHandler(cfg)
	if err != nil {
		slog.New(handler).Warn("failed to open log file, falling back to stderr only",
			"path", cfg.LogFile, "error", err)
		return handler, noop
	}
	return slog.NewMultiHandler(handler, fileHandler), cleanup
}

func newStderrHandler(cfg *Config) slog.Handler {
	opts := handlerOptions(cfg.Level.Level)
	if cfg.Format == "json" {
		return slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.NewTextHandler(os.Stderr, opts)
}

func newFileHandler(cfg *Config) (slog.Handler, func(), error) {
	dir := filepath.Dir(cfg.LogFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = f.Sync()
		_ = f.Close()
	}

	level := cfg.Level.Level
	if cfg.LogFileLevel != "" {
		level, _ = parseLevel(cfg.LogFileLevel)
	}
	opts := handlerOptions(level)

	format := cfg.LogFileFormat
	if format == "" {
		format = cfg.Format
	}
	if format == "text" {
		return slog.NewTextHandler(f, opts), cleanup, nil
	}
	return slog.NewJSONHandler(f, opts), cleanup, nil
}

func handlerOptions(level slog.Level) *slog.HandlerOptions {
	return &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, field slog.Attr) slog.Attr {
			if field.Key == slog.LevelKey && field.Value.Any().(slog.Level) == LevelTrace {
				field.Value = slog.StringValue("TRACE")
			}
			return field
		},
	}
}
