package skipper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

var testFunction = Function_builder{
	Namespace:  proto.String("skipper-production"),
	Deployment: proto.String("my-awesome-app-deployment"),
	Tenant:     proto.String("tenant-12345-abcdef"),
	Metadata:   proto.String("some-metadata-value"),
	Scale: Scale_builder{
		MinInstances:           proto.Uint32(1),
		MaxInstances:           proto.Uint32(10),
		TargetCpuUsageMilli:    proto.Uint32(500),
		TargetMemoryUsageMib:   proto.Uint32(256),
		TargetInFlightRequests: proto.Uint32(100),
	}.Build(),
}.Build()

var testFunctions = []*Function{
	Function_builder{Namespace: proto.String("ns1"), Deployment: proto.String("deploy1"), Tenant: proto.String("tenant1"), Metadata: proto.String("meta1"), Scale: Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10), TargetCpuUsageMilli: proto.Uint32(500), TargetMemoryUsageMib: proto.Uint32(256), TargetInFlightRequests: proto.Uint32(100)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns2"), Deployment: proto.String("deploy2"), Tenant: proto.String("tenant2"), Metadata: proto.String("meta2"), Scale: Scale_builder{MinInstances: proto.Uint32(2), MaxInstances: proto.Uint32(20), TargetCpuUsageMilli: proto.Uint32(600), TargetMemoryUsageMib: proto.Uint32(512), TargetInFlightRequests: proto.Uint32(200)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns3"), Deployment: proto.String("deploy3"), Tenant: proto.String("tenant3"), Metadata: proto.String("meta3"), Scale: Scale_builder{MinInstances: proto.Uint32(3), MaxInstances: proto.Uint32(30), TargetCpuUsageMilli: proto.Uint32(700), TargetMemoryUsageMib: proto.Uint32(1024), TargetInFlightRequests: proto.Uint32(300)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns4"), Deployment: proto.String("deploy4"), Tenant: proto.String("tenant4"), Metadata: proto.String("meta4"), Scale: Scale_builder{MinInstances: proto.Uint32(4), MaxInstances: proto.Uint32(40), TargetCpuUsageMilli: proto.Uint32(800), TargetMemoryUsageMib: proto.Uint32(2048), TargetInFlightRequests: proto.Uint32(400)}.Build()}.Build(),
	Function_builder{Namespace: proto.String("ns5"), Deployment: proto.String("deploy5"), Tenant: proto.String("tenant5"), Metadata: proto.String("meta5"), Scale: Scale_builder{MinInstances: proto.Uint32(5), MaxInstances: proto.Uint32(50), TargetCpuUsageMilli: proto.Uint32(900), TargetMemoryUsageMib: proto.Uint32(4096), TargetInFlightRequests: proto.Uint32(500)}.Build()}.Build(),
}

func TestHashNoCollisions(t *testing.T) {
	// Test that different identity fields produce different hashes (separator collision prevention)
	f1 := Function_builder{Namespace: proto.String("ab"), Deployment: proto.String("cd"), Tenant: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f2 := Function_builder{Namespace: proto.String("abc"), Deployment: proto.String("d"), Tenant: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f3 := Function_builder{Namespace: proto.String("a"), Deployment: proto.String("bcd"), Tenant: proto.String(""), Scale: Scale_builder{}.Build()}.Build()
	f4 := Function_builder{Namespace: proto.String("abcd"), Deployment: proto.String(""), Tenant: proto.String(""), Scale: Scale_builder{}.Build()}.Build()

	assert.Assert(t, f1.Hash() != f2.Hash(), "f1 and f2 should have different hashes")
	assert.Assert(t, f1.Hash() != f3.Hash(), "f1 and f3 should have different hashes")
	assert.Assert(t, f1.Hash() != f4.Hash(), "f1 and f4 should have different hashes")
	assert.Assert(t, f2.Hash() != f3.Hash(), "f2 and f3 should have different hashes")
	assert.Assert(t, f2.Hash() != f4.Hash(), "f2 and f4 should have different hashes")
	assert.Assert(t, f3.Hash() != f4.Hash(), "f3 and f4 should have different hashes")

	// Also test across Tenant field
	f5 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("ab"), Scale: Scale_builder{}.Build()}.Build()
	f6 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("abc"), Scale: Scale_builder{}.Build()}.Build()
	assert.Assert(t, f5.Hash() != f6.Hash(), "f5 and f6 should have different hashes")

	// Identical identity should have identical hashes regardless of metadata/scale
	f7 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Metadata: proto.String("meta-a"), Scale: Scale_builder{MaxInstances: proto.Uint32(1)}.Build()}.Build()
	f8 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Metadata: proto.String("meta-b"), Scale: Scale_builder{MaxInstances: proto.Uint32(99)}.Build()}.Build()
	assert.Assert(t, f7.Hash() == f8.Hash(), "same identity with different metadata/scale should have the same hash")

	// Oneshot changes identity
	f9 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Oneshot: proto.Bool(false), Scale: Scale_builder{}.Build()}.Build()
	f10 := Function_builder{Namespace: proto.String("ns"), Deployment: proto.String("dep"), Tenant: proto.String("tenant"), Oneshot: proto.Bool(true), Scale: Scale_builder{}.Build()}.Build()
	assert.Assert(t, f9.Hash() != f10.Hash(), "oneshot true vs false should have different hashes")
}

func BenchmarkHash(b *testing.B) {
	b.Run("XXHash", func(b *testing.B) {
		for b.Loop() {
			_ = testFunction.Hash()
		}
	})
}

func BenchmarkMapLookup(b *testing.B) {
	b.Run("HashKey", func(b *testing.B) {
		m := make(map[FunctionHash]int)
		for i, fn := range testFunctions {
			m[fn.Hash()] = i
		}

		lookupHash := testFunctions[2].Hash()

		for b.Loop() {
			_ = m[lookupHash]
		}
	})

	b.Run("HashKeyWithCompute", func(b *testing.B) {
		m := make(map[FunctionHash]int)
		for i, fn := range testFunctions {
			m[fn.Hash()] = i
		}

		lookupFn := testFunctions[2]

		for b.Loop() {
			_ = m[lookupFn.Hash()]
		}
	})
}

func TestFunctionFromHeader(t *testing.T) {
	validFn := Function_builder{
		Namespace:  proto.String("test-ns"),
		Deployment: proto.String("test-deploy"),
		Tenant:     proto.String("test-tenant"),
		Metadata:   proto.String("test-metadata"),
		Scale: Scale_builder{
			MinInstances:           proto.Uint32(1),
			MaxInstances:           proto.Uint32(10),
			TargetCpuUsageMilli:    proto.Uint32(500),
			TargetMemoryUsageMib:   proto.Uint32(256),
			TargetInFlightRequests: proto.Uint32(100),
		}.Build(),
	}.Build()

	tests := []struct {
		name    string
		header  string
		wantErr string
		wantFn  *Function
	}{
		{
			name:    "missing header",
			header:  "",
			wantErr: "missing " + FunctionKey.Header,
		},
		{
			name:    "invalid JSON",
			header:  "{invalid json}",
			wantErr: "failed to unmarshal " + FunctionKey.Header + " header:",
		},
		{
			name:    "missing namespace",
			header:  `{"deployment":"d","tenant":"t"}`,
			wantErr: "missing namespace",
		},
		{
			name:    "missing deployment",
			header:  `{"namespace":"n","tenant":"t"}`,
			wantErr: "missing deployment",
		},
		{
			name:    "missing tenant",
			header:  `{"namespace":"n","deployment":"d"}`,
			wantErr: "missing tenant",
		},
		{
			name:    "negative min instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative max instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"max_instances":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target cpu usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_cpu_usage_milli":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target memory usage",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_memory_usage_mib":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "negative target in flight requests",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"target_in_flight_requests":-1}}`,
			wantErr: "invalid value for uint32 field",
		},
		{
			name:    "nil scale",
			header:  `{"namespace":"n","deployment":"d","tenant":"t"}`,
			wantErr: "missing scale",
		},
		{
			name:    "zero max instances",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":0,"max_instances":0}}`,
			wantErr: "scale.max_instances must be >= 1",
		},
		{
			name:    "min greater than max",
			header:  `{"namespace":"n","deployment":"d","tenant":"t","scale":{"min_instances":5,"max_instances":3}}`,
			wantErr: "scale.min_instances (5) must be <= scale.max_instances (3)",
		},
		{
			name:   "valid function with scale",
			header: `{"namespace":"test-ns","deployment":"test-deploy","tenant":"test-tenant","metadata":"test-metadata","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`,
			wantFn: validFn,
		},
		{
			name:   "unknown fields are ignored",
			header: `{"namespace":"test-ns","deployment":"test-deploy","tenant":"test-tenant","metadata":"test-metadata","nonce":"abc123","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`,
			wantFn: validFn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set(FunctionKey.Header, tc.header)
			}

			fn, err := FunctionFromHeader(req)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				assert.Assert(t, fn == nil, "expected nil function on error")
			} else {
				assert.NilError(t, err)
				assert.Assert(t, proto.Equal(fn, tc.wantFn), "function mismatch: got %+v, want %+v", fn, tc.wantFn)
			}
		})
	}
}

func TestFunctionHeaderCacheBounded(t *testing.T) {
	// Fill cache beyond capacity and confirm it stays bounded.
	cap := 8
	c := newFunctionHeaderCache(cap)

	fn := Function_builder{
		Namespace:  proto.String("ns"),
		Deployment: proto.String("deploy"),
		Tenant:     proto.String("tenant"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	for i := range cap + 10 {
		c.Add(fmt.Sprintf("key-%d", i), fn)
	}

	assert.Equal(t, c.Len(), cap, "cache must not exceed its capacity")
}

func TestFunctionHeaderCacheLRUEviction(t *testing.T) {
	// Verify that the oldest (least recently used) entry is evicted when the
	// cache is full and a new entry is inserted.
	cap := 3
	c := newFunctionHeaderCache(cap)

	fn := Function_builder{
		Namespace:  proto.String("ns"),
		Deployment: proto.String("deploy"),
		Tenant:     proto.String("tenant"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	// Fill to capacity: key-0, key-1, key-2 (key-0 is LRU).
	for i := range cap {
		c.Add(fmt.Sprintf("key-%d", i), fn)
	}

	// Access key-0 to make it recently used; key-1 becomes LRU.
	_, ok := c.Get("key-0")
	assert.Assert(t, ok, "key-0 should be present before eviction")

	// Insert a new key; key-1 (LRU) should be evicted.
	c.Add("key-new", fn)

	_, stillThere := c.Get("key-1")
	assert.Assert(t, !stillThere, "key-1 (LRU) should have been evicted")

	_, ok0 := c.Get("key-0")
	assert.Assert(t, ok0, "key-0 (recently used) should still be present")

	_, ok2 := c.Get("key-2")
	assert.Assert(t, ok2, "key-2 should still be present")

	_, okNew := c.Get("key-new")
	assert.Assert(t, okNew, "key-new should be present")
}

func TestFunctionFromHeaderCacheIdentity(t *testing.T) {
	// FunctionFromHeader must return the same pointer for repeated calls with
	// the same header value.
	header := `{"namespace":"id-ns","deployment":"id-deploy","tenant":"id-tenant","scale":{"min_instances":1,"max_instances":5}}`

	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set(FunctionKey.Header, header)
	fn1, err := FunctionFromHeader(req1)
	assert.NilError(t, err)

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set(FunctionKey.Header, header)
	fn2, err := FunctionFromHeader(req2)
	assert.NilError(t, err)

	assert.Assert(t, fn1 == fn2, "same header must return the same pointer (cache identity)")
}

func TestFunctionHeaderCacheConcurrentAccess(t *testing.T) {
	// Verify concurrent reads and writes don't panic or corrupt the cache.
	t.Parallel()

	c := newFunctionHeaderCache(16)

	fn := Function_builder{
		Namespace:  proto.String("ns"),
		Deployment: proto.String("deploy"),
		Tenant:     proto.String("tenant"),
		Scale:      Scale_builder{MinInstances: proto.Uint32(1), MaxInstances: proto.Uint32(10)}.Build(),
	}.Build()

	done := make(chan struct{})
	for g := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range 200 {
				key := fmt.Sprintf("key-%d-%d", g, i%20)
				c.Add(key, fn)
				c.Get(key)
			}
		}()
	}
	for range 8 {
		<-done
	}

	assert.Assert(t, c.Len() <= 16, "cache must not exceed its capacity under contention")
}

// Ensure xxhash import is used by benchmarks
var _ = xxhash.New

var sinkFunction *Function

func BenchmarkFunctionFromHeader(b *testing.B) {
	const validHeader = `{"namespace":"test-ns","deployment":"test-deploy","tenant":"test-tenant","metadata":"test-metadata","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`

	b.Run("cache_hit", func(b *testing.B) {
		b.ReportAllocs()

		// Pre-warm the cache.
		warmReq := httptest.NewRequest(http.MethodGet, "/", nil)
		warmReq.Header.Set(FunctionKey.Header, validHeader)
		if _, err := FunctionFromHeader(warmReq); err != nil {
			b.Fatal(err)
		}

		b.RunParallel(func(pb *testing.PB) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(FunctionKey.Header, validHeader)
			for pb.Next() {
				sinkFunction, _ = FunctionFromHeader(req)
			}
		})
	})

	b.Run("cache_miss", func(b *testing.B) {
		b.ReportAllocs()

		var counter atomic.Int64
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				n := counter.Add(1)
				header := fmt.Sprintf(`{"namespace":"test-ns","deployment":"test-deploy","tenant":"tenant-%d","metadata":"test-metadata","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`, n)
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set(FunctionKey.Header, header)
				sinkFunction, _ = FunctionFromHeader(req)
			}
		})
	})
}
