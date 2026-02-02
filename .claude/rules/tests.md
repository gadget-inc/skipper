---
paths:
    - "internal/**/*_test.go"
---

# Testing Guidelines

## Table-Driven Tests

Prefer adding cases to existing table-driven tests. Two common patterns:

```go
// Pattern 1: Simple input/output tests
testCases := []struct {
    name    string
    input   string
    wantErr string  // empty for success
    want    *Type
}{...}

for _, tc := range testCases {
    t.Run(tc.name, func(t *testing.T) {
        // ...
    })
}

// Pattern 2: Stateful tests with setup/action/check
testCases := []struct {
    name   string
    setup  func(*testing.T, *testState)
    change func(*testing.T, *testState)
    check  func(*testing.T, *testState)
}{...}

for _, tc := range testCases {
    t.Run(tc.name, func(t *testing.T) {
        // ...
    })
}
```

## Assertions

Common `gotest.tools/v3/assert` patterns:

```go
assert.NilError(t, err)
assert.ErrorContains(t, err, "substring")
assert.Assert(t, condition)
assert.Equal(t, got, want)       // for comparable types
assert.DeepEqual(t, got, want)   // for structs, slices, maps
```

## Golden Files

Use `gotest.tools/v3/golden` for snapshot testing. Name fixtures with `golden` prefix:

```go
var goldenFunction = &Function{...}

// Write golden file
golden.Assert(t, string(output)+"\n", "filename.golden")

// Read golden file
data := golden.Get(t, "filename.golden")
```

## Test Helpers

- Use `t.Helper()` at the start of helper functions
- Use `t.Parallel()` for independent tests
- Place package-specific helpers in the test file or `testutil_test.go`
- Use `internal/fixture/` for cross-package test data
- Name package-level fixtures with `test` prefix (e.g., `testFunction`)

## Benchmarks

```go
func BenchmarkFoo(b *testing.B) {
    b.Run("case", func(b *testing.B) {
        for b.Loop() {
            // benchmarked code
        }
    })
}
```
