# Optimize Hot Path

Benchmark-driven optimization cycle: measure, optimize, verify.

## Workflow

### 1. Identify the target

Either from [Analyze Profiles](./analyze-profiles.md) or user direction. Confirm:

- Which function/package to optimize
- What metric matters (CPU time, allocations, throughput under contention)

### 2. Check for existing benchmarks

```bash
grep -r "func Benchmark" internal/<package>/
```

If no benchmark covers the target, write one first. See [Benchmark Patterns](./benchmark-patterns.md) for Skipper-specific examples.

### 3. Establish baseline

Run the benchmark with enough iterations for statistical significance:

```bash
mkdir -p tmp/benchmarks
tests -bench=BenchmarkName -benchmem -count=6 ./internal/<package>/... | tee tmp/benchmarks/<name>-before.txt
```

Review the baseline numbers. Note the ns/op, B/op, and allocs/op.

### 4. Implement the optimization

Make the change. Keep all existing tests passing:

```bash
tests -short ./internal/<package>/...
```

Common optimization patterns:

- **Reduce allocations**: Use `sync.Pool`, pre-allocate slices, avoid `fmt.Sprintf` in hot paths
- **Reduce copies**: Pass pointers, use `[]byte` instead of `string` where possible
- **Better algorithms**: Use binary search instead of linear scan, cache computed values
- **Reduce lock contention**: Use `sync.RWMutex`, shard data, use atomic operations

### 5. Measure the optimization

```bash
tests -bench=BenchmarkName -benchmem -count=6 ./internal/<package>/... | tee tmp/benchmarks/<name>-after.txt
```

### 6. Compare with benchstat

```bash
benchstat tmp/benchmarks/<name>-before.txt tmp/benchmarks/<name>-after.txt
```

**Require statistical significance** before declaring an improvement. Look for:

- `p < 0.05` in the p-value column
- Consistent direction (all runs faster, not just average)
- Meaningful magnitude (>5% improvement for CPU, >0 for allocations)

If results are noisy, increase `-count` to 10 or 20.

### 7. Report results

Include the benchstat output showing before/after comparison. Note:

- What changed and why
- Any trade-offs (memory vs CPU, code complexity)
- Whether the change affects correctness (all tests still pass)
