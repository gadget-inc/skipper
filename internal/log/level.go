package log

import (
	"context"
	"log/slog"
)

const (
	LevelTrace = slog.Level(-8)
)

func Trace(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, LevelTrace, msg, fields...)
}

func Debug(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelDebug, msg, fields...)
}

func Info(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelInfo, msg, fields...)
}

func Warn(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelWarn, msg, fields...)
}

func Error(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelError, msg, fields...)
}
