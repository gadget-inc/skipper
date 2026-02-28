package telemetry

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/proto"
)

var sinkCtx context.Context

func BenchmarkWith(b *testing.B) {
	fn := skipper.Function_builder{
		Tenant:     proto.String("bench-tenant"),
		Metadata:   proto.String("bench-metadata"),
		Namespace:  proto.String("bench-ns"),
		Deployment: proto.String("bench-deploy"),
	}.Build()

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/test", nil)

	b.Run("1 attr", func(b *testing.B) {
		b.ReportAllocs()
		a := key.Namespace.Attr("bench-ns")
		ctx := context.Background()
		for b.Loop() {
			sinkCtx = With(ctx, a)
		}
	})

	b.Run("3 attrs with groups", func(b *testing.B) {
		b.ReportAllocs()
		a1 := key.Function.Attr(fn)
		a2 := key.Namespace.Attr("bench-ns")
		a3 := key.Attempt.Attr(1)
		ctx := context.Background()
		for b.Loop() {
			sinkCtx = With(ctx, a1, a2, a3)
		}
	})

	b.Run("5 attrs with groups", func(b *testing.B) {
		b.ReportAllocs()
		a1 := key.Function.Attr(fn)
		a2 := key.Namespace.Attr("bench-ns")
		a3 := key.Attempt.Attr(1)
		a4 := key.Request.Attr(req)
		a5 := key.Duration.Attr(time.Second)
		ctx := context.Background()
		for b.Loop() {
			sinkCtx = With(ctx, a1, a2, a3, a4, a5)
		}
	})
}
