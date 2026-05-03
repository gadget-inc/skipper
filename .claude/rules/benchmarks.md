# Benchmark Guidelines

Scoped to: `internal/**/*_test.go`

## Writing Benchmarks

- Use `b.Loop()` (Go 1.24+) instead of `for i := 0; i < b.N; i++`
- Use `b.ReportAllocs()` for allocation-sensitive paths
- Use `b.RunParallel` for contention benchmarks
- Use `var sink` at package level to prevent compiler elimination of results
- Save results to `tmp/benchmarks/` for `benchstat` comparison

## Running and Comparing

```bash
# Run benchmarks and save results
dev tests go -bench=BenchmarkName -benchmem -count=6 ./internal/pkg/... | tee tmp/benchmarks/name-before.txt

# After optimization, run again
dev tests go -bench=BenchmarkName -benchmem -count=6 ./internal/pkg/... | tee tmp/benchmarks/name-after.txt

# Compare with benchstat (require statistical significance)
benchstat tmp/benchmarks/name-before.txt tmp/benchmarks/name-after.txt
```

## Example

```go
var sink string // prevent compiler elimination

func BenchmarkFoo(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		sink = doWork()
	}
}

func BenchmarkFooParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = doWork()
		}
	})
}
```
