package skipper

import (
	"runtime"
	"sync"
	"testing"
	"time"

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

			// Compare resolved slog values: the cache pre-resolves the
			// LogValuer to break *Function retention, which changes the
			// Kind of the cached Value but not the final log output.
			assert.Equal(t, got.Slog.Key, want.Slog.Key)
			assert.Assert(t, got.Slog.Value.Resolve().Equal(want.Slog.Value.Resolve()),
				"Slog mismatch:\n got: %v\nwant: %v", got.Slog.Value.Resolve(), want.Slog.Value.Resolve())
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

	wantResolved := key.Function.Attr(fn).Slog.Value.Resolve()

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				got := fn.Attr()
				if !got.Slog.Value.Resolve().Equal(wantResolved) {
					t.Errorf("Slog mismatch under concurrent access")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestFunctionAttrCacheShrinksAfterGC(t *testing.T) {
	// Insert entries for N distinct Functions, then drop all strong
	// references to them. The weak cache must release those entries after
	// GC -- otherwise we leak one entry per Function ever parsed.
	const n = 100

	start := functionAttrCacheSize()
	for range n {
		fn := Function_builder{
			Namespace:  proto.String("gc-ns"),
			Deployment: proto.String("gc-deploy"),
			Tenant:     proto.String("gc-tenant"),
			Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(1)}.Build(),
		}.Build()
		_ = fn.Attr()
	}
	if diff := functionAttrCacheSize() - start; diff != n {
		t.Fatalf("expected %d new entries pre-GC, got %d", n, diff)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		if functionAttrCacheSize() <= start {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("cache did not shrink after GC: size=%d, started at %d", functionAttrCacheSize(), start)
}

func functionAttrCacheSize() int {
	n := 0
	functionAttrCache.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
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
