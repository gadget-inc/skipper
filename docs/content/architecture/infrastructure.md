---
title: Infrastructure
description: Logging, configuration, and gRPC communication internals in Skipper.
---

Cross-cutting infrastructure that supports both the controller and router: structured logging with trace correlation, declarative configuration binding, and gRPC service communication.

## Logging architecture

Logging is context-based: `log.With(ctx, fields...)` accumulates structured fields that propagate through the call chain. All log functions take context as the first argument: `log.Info(ctx, "message", fields...)`.

### Type-safe attribute keys

Attribute keys are defined in `internal/key/`. Each key provides both `.Slog()` and `.Otel()` methods, ensuring consistent naming across logs and traces. This eliminates string duplication and typos when the same attribute appears in both systems.

### Log-trace correlation

Error-level log records automatically include `trace_id` and `span_id` when a span exists in context. Errors are also recorded on the active span via `span.SetStatus(codes.Error)`, enabling bidirectional navigation between logs and traces.

## Configuration system

The `internal/config/` package provides declarative flag binding via struct tags.

### Supported tags

| Tag           | Purpose                                     |
| ------------- | ------------------------------------------- |
| `flag`        | Flag name (e.g., `heartbeat-timeout`)       |
| `description` | Help text                                   |
| `default`     | Default value                               |
| `required`    | Mark as required                            |
| `sensitive`   | Mask value in help output as "**\*\*\*\***" |
| `separator`   | Delimiter for slice values                  |

### Resolution order

1. CLI flags (highest priority)
2. Environment variables (`--my-flag` becomes `SKIPPER_MY_FLAG`)

Supported types: scalars, slices, durations, URLs, maps, and any type implementing `encoding.TextUnmarshaler`.

## gRPC communication

The controller exposes a gRPC service on port 50051 (default). Routers connect using DNS-based service discovery with round-robin load balancing (`dns:///service:port`).

### Retry policies

{{< grpcRetryTable >}}

All retries trigger only on the UNAVAILABLE status code. Every RPC is instrumented with OpenTelemetry.
