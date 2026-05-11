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

// cachedAssignmentKeys are the production-used cached Assignment keys; both
// participate in dual-emit and both need their cached-path output pinned.
var cachedAssignmentKeys = []struct {
	name string
	key  *key.Key[*Assignment]
}{
	{name: "AssignmentKey", key: AssignmentKey},
	{name: "LegacyFunctionKey", key: LegacyFunctionKey},
}

// TestAssignmentKeyEquivalence pins each cached *Assignment key's output to
// an equivalent uncached key so caching cannot silently drift the span
// attribute keys/values for either key in the dual-emit pair.
func TestAssignmentKeyEquivalence(t *testing.T) {
	t.Parallel()

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

	for _, kc := range cachedAssignmentKeys {
		uncached := key.New(kc.key.Name, (*Assignment).LogValue)
		for _, tc := range testCases {
			t.Run(kc.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				got := kc.key.Attr(tc.fn)
				want := uncached.Attr(tc.fn)

				assert.Assert(t, got.Slog.Equal(want.Slog), "Slog mismatch:\n got: %v\nwant: %v", got.Slog, want.Slog)
				assert.DeepEqual(t, got.Otel, want.Otel, cmpopts.EquateComparable(attribute.KeyValue{}))
			})
		}
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

	const goroutines = 32
	const iterations = 100

	for _, kc := range cachedAssignmentKeys {
		t.Run(kc.name, func(t *testing.T) {
			t.Parallel()

			want := kc.key.Attr(fn)

			var wg sync.WaitGroup
			wg.Add(goroutines)
			for range goroutines {
				go func() {
					defer wg.Done()
					for range iterations {
						if got := kc.key.Attr(fn); !got.Slog.Equal(want.Slog) {
							t.Errorf("Slog mismatch under concurrent access")
							return
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

func BenchmarkAssignmentKeyAttr(b *testing.B) {
	fn := Assignment_builder{
		Namespace:  new("bench-ns"),
		Deployment: new("bench-deploy"),
		Tenant:     new("bench-tenant"),
		Metadata:   new("bench-meta"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	for _, kc := range cachedAssignmentKeys {
		b.Run(kc.name, func(b *testing.B) {
			_ = kc.key.Attr(fn) // prime the cache so we measure the hit path

			b.ReportAllocs()
			for b.Loop() {
				sinkAttrResult = kc.key.Attr(fn)
			}
		})
	}
}

var sinkAttrResult key.Attr
