package log

import (
	"context"
	"log/slog"
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
