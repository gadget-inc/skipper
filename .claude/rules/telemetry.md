---
paths:
    - "**/*.go"
---

# Telemetry Guidelines

The `internal/key/` package provides type-safe attributes for logging and tracing with consistent naming.

## Logging

Use `key.X.Slog()` instead of raw slog attributes:

```go
// Correct
log.Info(ctx, "forwarding request", key.Function.Slog(fn), key.Instance.Slog(instance))
log.Error(ctx, "request failed", key.Error.Slog(err))

// Wrong - loses type safety and consistent naming
slog.Info("forwarding request", "function", fn, "instance", instance)
```

## Tracing

Use `key.X.Otel()` for span attributes:

```go
span.SetAttributes(key.Function.Otel(fn)...)
span.SetAttributes(key.Namespace.Otel(ns)...)
```

## Combined (Logs + Traces)

Use `key.X.Attr()` with `telemetry.With()` when you need both:

```go
ctx := telemetry.With(ctx,
    key.Function.Attr(fn),
    key.Instance.Attr(instance),
)
```

## Available Keys

See `internal/key/keys.go`. Common keys:

- Identifiers: `Namespace`, `Deployment`, `Tenant`, `Metadata`
- Domain: `Function`, `Instance`, `Heartbeat`, `Scale`
- HTTP: `Request`, `Response`, `URL`, `Addr`
- Metrics: `Error`, `Duration`, `Count`, `CPUUsageMilli`, `MemoryUsageMiB`
