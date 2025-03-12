package log

import (
	"context"
	"log/slog"
)

type (
	contextKey struct{}
	Hook       func(ctx context.Context, record *slog.Record)
)

var (
	key   = contextKey{}
	hooks []Hook
)

func AddHook(hook Hook) {
	hooks = append(hooks, hook)
}

type slogHandler struct {
	slog.Handler
}

var _ slog.Handler = slogHandler{}

func (sh slogHandler) Handle(ctx context.Context, record slog.Record) error {
	if fields, ok := ctx.Value(key).([]slog.Attr); ok {
		record.AddAttrs(fields...)
	}
	for _, hook := range hooks {
		hook(ctx, &record)
	}
	return sh.Handler.Handle(ctx, record)
}
