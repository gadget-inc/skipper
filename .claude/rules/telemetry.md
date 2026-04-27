---
paths:
    - "**/*.go"
---

# Telemetry Guidelines

Typed keys provide consistent, type-safe attribute names across slog, OTel, HTTP headers, and Kubernetes labels.

- Framework-level and primitive keys live in `internal/key/` (e.g. `key.Namespace`, `key.Count`, `key.Request`).
- Domain-specific keys live next to the type they describe (e.g. `skipper.FunctionKey`, `skipper.HeartbeatKey`).

Both flavors expose the same `Slog` and `Attr` methods. Pick the matching key for the value's type — passing the wrong type fails at compile time.

## Logging

Use the key's `.Slog()` method instead of raw slog attributes:

```go
// Correct
log.Info(ctx, "forwarding request", skipper.FunctionKey.Slog(fn), skipper.InstanceKey.Slog(instance))
log.Error(ctx, "request failed", key.Error.Slog(err))

// Wrong - loses type safety and consistent naming
slog.Info("forwarding request", "function", fn, "instance", instance)
```

## Tracing

Use `.Attr(v).Otel` for span attributes:

```go
span.SetAttributes(skipper.FunctionKey.Attr(fn).Otel...)
span.SetAttributes(key.Namespace.Attr(ns).Otel...)
```

## Combined (Logs + Traces)

Use `.Attr()` with `telemetry.With()` when you need both:

```go
ctx := telemetry.With(ctx,
    skipper.FunctionKey.Attr(fn),
    skipper.InstanceKey.Attr(instance),
)
```

## Declaring new keys

Generic and external-type keys belong in `internal/key/keys.go`. Domain keys belong next to their struct in the owning package, declared via `key.New` (or `key.NewCached` if pointer-keyed memoization makes sense for the type):

```go
// internal/skipper/widget.go
var WidgetKey = key.New("widget", (*Widget).LogValue)
```

For a hot path that benefits from per-pointer caching:

```go
var WidgetKey = key.NewCached("widget", (*Widget).LogValue)
```

For a hot path where `[]attribute.KeyValue` should be built directly (bypassing the slog walk):

```go
var WidgetKey = key.NewWithOtel("widget", (*Widget).LogValue, (*Widget).otelAttrs)
```

## Available Keys

- Cross-cutting (in `internal/key/`): `Namespace`, `Deployment`, `Tenant`, `Metadata`, `Count`, `Duration`, `Error`, `Attempt`, `Reason`, `Request`, `Response`, `URL`, `Pod`, `K8sReplicaSet`, `CPUUsageMilli`, `MemoryUsageMiB`, ...
- Domain (in `internal/skipper/`): `FunctionKey`, `HeartbeatKey`, `InstanceKey`, `ScaleKey`, `ScaleDecisionKey`.
