# Benchmark Patterns

Skipper-specific patterns for writing benchmarks. See also `.claude/rules/benchmarks.md` for general guidelines.

## Hash Ring Get

The hash ring is on the hot path for every request through the router.

```go
func BenchmarkHashRingGet(b *testing.B) {
	b.Run("4-ips", func(b *testing.B) {
		ring := New()
		for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"} {
			ring.Add(ip)
		}
		key := testKey("benchmark-key")
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			ring.Get(key)
		}
	})
}
```

For contention benchmarks, use `b.RunParallel`:

```go
b.Run("4-ips/parallel", func(b *testing.B) {
	ring := New()
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		ring.Add(ip)
	}
	key := testKey("benchmark-key")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ring.Get(key)
		}
	})
})
```

## FunctionHash

```go
func BenchmarkFunctionHash(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		testFunction.Hash()
	}
}
```

## FunctionFromHeader

Uses realistic HTTP request fixtures:

```go
func BenchmarkFunctionFromHeader(b *testing.B) {
	header := `{"namespace":"ns","deployment":"dep","tenant":"t","metadata":"m","scale":{"min_instances":1,"max_instances":10,"target_cpu_usage_milli":500,"target_memory_usage_mib":256,"target_in_flight_requests":100}}`
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(key.Function.Header, header)
		_, _ = FunctionFromHeader(req)
	}
}
```

## Buffer Pool

Sequential and parallel variants to measure contention:

```go
func BenchmarkBufferPool(b *testing.B) {
	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			buf := bufferPool.Get()
			bufferPool.Put(buf)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := bufferPool.Get()
				bufferPool.Put(buf)
			}
		})
	})
}
```

## Tips

- **Realistic fixtures**: Use production-like data sizes and shapes (e.g., real namespace/deployment names, realistic IP counts)
- **`b.ReportAllocs()`**: Always include for allocation-sensitive paths
- **`b.ResetTimer()`**: Call after expensive setup that shouldn't be measured
- **Sink variables**: Use package-level `var sink T` to prevent the compiler from eliminating results
- **Sub-benchmarks**: Use `b.Run("case", ...)` to test different scenarios (e.g., ring sizes, key distributions)
