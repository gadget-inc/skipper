package telemetry

import (
	"context"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"go.opentelemetry.io/otel/attribute"
)

func With(ctx context.Context, attrs ...key.Attr) context.Context {
	slogAttrs := make([]slog.Attr, 0, len(attrs))
	otelAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		slogAttrs = append(slogAttrs, attr.Slog)
		otelAttrs = append(otelAttrs, attr.Otel...)
	}
	ctx = log.With(ctx, slogAttrs...)
	ctx = WithPropagatedAttributes(ctx, otelAttrs...)
	return ctx
}
