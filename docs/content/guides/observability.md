---
title: Observability
description: Tracing, metrics, and logging in Skipper.
---

Skipper provides three observability pillars: OpenTelemetry tracing, Prometheus metrics, and structured logging. All are configurable and can be enabled independently.

## Enabling telemetry

<div class="table-scroll">

| Flag                          | Default             | Purpose                                           |
| ----------------------------- | ------------------- | ------------------------------------------------- |
| `--telemetry`                 | false               | Master switch for all telemetry                   |
| `--telemetry-trace`           | true (when enabled) | Enable distributed tracing                        |
| `--telemetry-metric`          | true (when enabled) | Enable Prometheus metrics                         |
| `--telemetry-metric-otlp`     | false               | Export metrics via OTLP in addition to Prometheus |
| `--telemetry-prometheus-host` | 0.0.0.0             | Prometheus scrape endpoint host                   |
| `--telemetry-prometheus-port` | 9090                | Prometheus scrape endpoint port                   |

</div>

## Tracing

Skipper uses the OpenTelemetry SDK with an OTLP exporter. The service name is `skipper.controller` or `skipper.router` depending on the component. Resource metadata includes container, environment, process, and runtime information.

Key features:

- Errors logged at error level are automatically recorded on the active span.
- Trace ID and span ID are added to all error-level log records for correlation.
- Context attributes propagate to child spans automatically.

The router's HTTP transport and the controller's gRPC server are both instrumented for automatic distributed tracing. `/healthz` is excluded from tracing to reduce noise.

## Prometheus metrics

### Controller metrics (prefix: `skipper_controller_`)

<div class="table-scroll">

{{< metricsTable controller >}}

</div>

### Router metrics (prefix: `skipper_router_`)

<div class="table-scroll">

{{< metricsTable router >}}

</div>

### Key insights

- `waiting_for_unassigned_pods` > 0 indicates pod supply issues. The underlying deployment may need more replicas.
- `informer_event_lag_seconds` spikes suggest API server or network pressure.
- `requests_in_flight` correlates directly with scaling decisions when in-flight request scaling is configured.
- Compare `scale_ups_total` vs `scale_downs_total` over time to understand scaling churn.

## Structured logging

Default format is JSON (`--log-format`, options: `json` or `text`). Log levels: trace, debug, info, warn, error (`--log-level`, default: info). A custom TRACE level below DEBUG is available for very verbose output.

Structured fields propagate through the call chain, so a log entry inherits context from its parent operation. Attribute names are consistent between logs and traces.

## Log-trace correlation

Error-level logs automatically include `trace_id` and `span_id` fields when a trace is active, enabling you to jump from a log entry directly to its distributed trace. Errors are also recorded on the active span for bidirectional correlation.
