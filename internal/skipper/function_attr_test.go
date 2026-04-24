package skipper

import (
	"sync"
	"testing"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

func TestFunctionAttrEquivalence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		fn   *Function
	}{
		{
			name: "fully populated",
			fn:   testFunction,
		},
		{
			name: "minimal required fields",
			fn: Function_builder{
				Namespace:  proto.String("ns"),
				Deployment: proto.String("deploy"),
				Tenant:     proto.String("tenant"),
				Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(1)}.Build(),
			}.Build(),
		},
		{
			name: "oneshot true",
			fn: Function_builder{
				Namespace:  proto.String("ns"),
				Deployment: proto.String("deploy"),
				Tenant:     proto.String("tenant"),
				Oneshot:    proto.Bool(true),
				Scale:      Scale_builder{MinInstances: proto.Uint32(0), MaxInstances: proto.Uint32(10)}.Build(),
			}.Build(),
		},
		{
			name: "empty metadata",
			fn: Function_builder{
				Namespace:  proto.String("ns"),
				Deployment: proto.String("deploy"),
				Tenant:     proto.String("tenant"),
				Metadata:   proto.String(""),
				Scale:      Scale_builder{MaxInstances: proto.Uint32(5)}.Build(),
			}.Build(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.fn.Attr()
			want := key.Function.Attr(tc.fn)

			assert.Assert(t, got.Slog.Equal(want.Slog), "Slog mismatch:\n got: %v\nwant: %v", got.Slog, want.Slog)
			assert.DeepEqual(t, got.Otel, want.Otel, cmpopts.EquateComparable(attribute.KeyValue{}))
		})
	}
}

func TestFunctionAttrConcurrent(t *testing.T) {
	t.Parallel()

	fn := Function_builder{
		Namespace:  proto.String("concurrent-ns"),
		Deployment: proto.String("concurrent-deploy"),
		Tenant:     proto.String("concurrent-tenant"),
		Metadata:   proto.String("concurrent-meta"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	want := key.Function.Attr(fn)

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				got := fn.Attr()
				if !got.Slog.Equal(want.Slog) {
					t.Errorf("Slog mismatch under concurrent access")
					return
				}
				if len(got.Otel) != len(want.Otel) {
					t.Errorf("Otel length mismatch under concurrent access: got %d want %d", len(got.Otel), len(want.Otel))
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkFunctionAttr(b *testing.B) {
	fn := Function_builder{
		Namespace:  proto.String("bench-ns"),
		Deployment: proto.String("bench-deploy"),
		Tenant:     proto.String("bench-tenant"),
		Metadata:   proto.String("bench-meta"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	// Prime the cache so we measure the hit path, not the first-call build.
	_ = fn.Attr()

	b.ReportAllocs()
	for b.Loop() {
		sinkAttrResult = fn.Attr()
	}
}

var sinkAttrResult key.Attr
