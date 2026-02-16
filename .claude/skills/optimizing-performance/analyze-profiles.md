# Analyze Profiles

Inspect committed PGO profiles or fetched profiles to identify optimization candidates.

## Workflow

### 1. Get the top-level view

Run flat and cumulative views to understand where time is spent:

```bash
# Flat: where does the CPU actually spend time?
profile analyze --pgo

# Cumulative: which call trees are expensive?
profile analyze --pgo --cum
```

For router profiles, add `-c router`.

### 2. Filter to actionable functions

Focus on functions under `internal/` — runtime and stdlib functions (like `runtime.mallocgc`, `syscall.read`) are not directly optimizable. Look for:

- Functions with high flat% (direct CPU consumers)
- Functions with high cum% but low flat% (orchestrators calling expensive code)
- Functions appearing in both views (both direct consumers and in hot paths)

### 3. Drill into candidates

For each candidate function, get caller/callee context and source-level attribution:

```bash
# Who calls this function, and what does it call?
profile analyze --pgo --mode=peek -f FunctionName

# Where exactly in the source code is time spent?
profile analyze --pgo --mode=source -f FunctionName
```

### 4. Classify the optimization opportunity

Each hotspot typically falls into one of these categories:

| Category | Signals | Typical Fix |
|----------|---------|-------------|
| Allocation reduction | `runtime.mallocgc` in callees, high alloc counts | Object pooling, pre-allocation, reducing copies |
| Algorithm improvement | High flat% in a single function | Better data structures, caching, algorithmic change |
| Concurrency bottleneck | Lock contention in peek view, `sync.Mutex` in callees | Sharding, lock-free structures, reducing critical sections |
| Hot loop | High iteration count in source view | Loop hoisting, batching, early exits |

### 5. Report findings

Summarize the top 5 hotspots with:
- Function name and package
- Flat% and cum% from the profile
- Classification (from step 4)
- Recommended optimization approach
- Whether an existing benchmark covers it

Then proceed to [Optimize Hot Path](./optimize-hot-path.md) for the most impactful candidate.
