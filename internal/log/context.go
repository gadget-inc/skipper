package log

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey struct{}

var key = ctxKey{}

type ctxHandler struct {
	slog.Handler
}

func (h ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if fields, ok := ctx.Value(key).([]slog.Attr); ok {
		r.AddAttrs(fields...)
	}
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}
