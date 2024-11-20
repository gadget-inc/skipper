package log

import (
	"context"
	"log/slog"
)

const (
	LevelTrace = slog.Level(-8)
)

type ctxKey struct{}

var key = ctxKey{}

type ctxHandler struct {
	slog.Handler
}

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	fields, ok := ctx.Value(key).([]slog.Attr)
	if ok {
		r.AddAttrs(fields...)
	}
	return h.Handler.Handle(ctx, r)
}

func Init(h slog.Handler) {
	slog.SetDefault(slog.New(ctxHandler{Handler: h}))
}

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

func Fatal(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelError, msg, fields...)
}

func With(ctx context.Context, fields ...slog.Attr) context.Context {
	existingFields, ok := ctx.Value(key).([]slog.Attr)
	if ok {
		fields = append(existingFields, fields...)
	}
	return context.WithValue(ctx, key, fields)
}
