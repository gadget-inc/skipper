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

// Compile-time gate: each domain key binds to its concrete *T.
var (
	_ *key.Key[*Assignment]    = LegacyFunctionKey
	_ *key.Key[*Heartbeat]     = HeartbeatKey
	_ *key.Key[*Instance]      = InstanceKey
	_ *key.Key[*Scale]         = ScaleKey
	_ *key.Key[*ScaleDecision] = ScaleDecisionKey
)

// TestAssignmentKeyEquivalence pins the cached path's output to the uncached
// path so caching cannot silently drift the span attribute keys/values.
func TestAssignmentKeyEquivalence(t *testing.T) {
	t.Parallel()

	uncached := key.New("assignment", (*Assignment).LogValue)

	testCases := []struct {
		name string
		fn   *Assignment
	}{
		{name: "fully populated", fn: testAssignment},
		{
			name: "minimal required fields",
			fn: Assignment_builder{
				Namespace:  new("ns"),
				Deployment: new("deploy"),
				Tenant:     new("tenant"),
				Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(1)}.Build(),
			}.Build(),
		},
		{
			name: "oneshot true",
			fn: Assignment_builder{
				Namespace:  new("ns"),
				Deployment: new("deploy"),
				Tenant:     new("tenant"),
				Oneshot:    new(true),
				Scale:      Scale_builder{MinInstances: proto.Uint32(0), MaxInstances: proto.Uint32(10)}.Build(),
			}.Build(),
		},
		{
			name: "empty metadata",
			fn: Assignment_builder{
				Namespace:  new("ns"),
				Deployment: new("deploy"),
				Tenant:     new("tenant"),
				Metadata:   new(""),
				Scale:      Scale_builder{MaxInstances: proto.Uint32(5)}.Build(),
			}.Build(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := AssignmentKey.Attr(tc.fn)
			want := uncached.Attr(tc.fn)

			assert.Assert(t, got.Slog.Equal(want.Slog), "Slog mismatch:\n got: %v\nwant: %v", got.Slog, want.Slog)
			assert.DeepEqual(t, got.Otel, want.Otel, cmpopts.EquateComparable(attribute.KeyValue{}))
		})
	}
}

func TestAssignmentKeyConcurrent(t *testing.T) {
	t.Parallel()

	fn := Assignment_builder{
		Namespace:  new("concurrent-ns"),
		Deployment: new("concurrent-deploy"),
		Tenant:     new("concurrent-tenant"),
		Metadata:   new("concurrent-meta"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	want := AssignmentKey.Attr(fn)

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				if got := AssignmentKey.Attr(fn); !got.Slog.Equal(want.Slog) {
					t.Errorf("Slog mismatch under concurrent access")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkAssignmentKeyAttr(b *testing.B) {
	fn := Assignment_builder{
		Namespace:  new("bench-ns"),
		Deployment: new("bench-deploy"),
		Tenant:     new("bench-tenant"),
		Metadata:   new("bench-meta"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	_ = AssignmentKey.Attr(fn) // prime the cache so we measure the hit path

	b.ReportAllocs()
	for b.Loop() {
		sinkAttrResult = AssignmentKey.Attr(fn)
	}
}

var sinkAttrResult key.Attr
