package telemetry

import (
	"context"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"go.opentelemetry.io/otel/attribute"
)

func With(ctx context.Context, attrs ...key.Attr) context.Context {
	// Pre-compute exact OTel attribute count to avoid slice regrowth
	// (each key.Attr can expand to multiple OTel attributes for groups).
	otelCount := 0
	for _, attr := range attrs {
		otelCount += len(attr.Otel)
	}

	slogAttrs := make([]slog.Attr, len(attrs))
	otelAttrs := make([]attribute.KeyValue, 0, otelCount)
	for i, attr := range attrs {
		slogAttrs[i] = attr.Slog
		otelAttrs = append(otelAttrs, attr.Otel...)
	}
	ctx = log.With(ctx, slogAttrs...)
	ctx = WithPropagatedAttributes(ctx, otelAttrs...)
	return ctx
}
