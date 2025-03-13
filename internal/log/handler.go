package log

import (
	"context"
	"log/slog"
)

type slogHandler struct {
	slog.Handler
}

var _ slog.Handler = slogHandler{}

func (sh slogHandler) Handle(ctx context.Context, record slog.Record) error {
	if fields, ok := ctx.Value(ctxKey).([]slog.Attr); ok {
		record.AddAttrs(fields...)
	}
	for _, hook := range hooks {
		hook(ctx, &record)
	}
	return sh.Handler.Handle(ctx, record)
}
