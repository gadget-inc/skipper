package log

import (
	"context"
	"log/slog"
)

type (
	contextKey struct{}
)

var ctxKey = contextKey{}

func With(ctx context.Context, fields ...slog.Attr) context.Context {
	if contextFields, ok := ctx.Value(ctxKey).([]slog.Attr); ok {
		fields = append(contextFields, fields...)
	}
	return context.WithValue(ctx, ctxKey, fields)
}
