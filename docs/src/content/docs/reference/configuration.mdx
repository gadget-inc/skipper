---
title: Configuration Reference
description: Complete reference for all controller and router configuration options.
---

Every flag has an environment variable equivalent: `--flag-name` becomes `SKIPPER_FLAG_NAME`. CLI flags take precedence over environment variables. Sensitive values (private keys) are shown as `********` in help output.

## Controller configuration

| Flag                                  | Type     | Default               | Description                                                 |
| ------------------------------------- | -------- | --------------------- | ----------------------------------------------------------- |
| `--host`                              | string   | `0.0.0.0`             | Hostname the controller listens on                          |
| `--port`                              | int      | `50051`               | gRPC server port                                            |
| `--shutdown-timeout`                  | duration | `5s`                  | Graceful shutdown timeout                                   |
| `--namespace`                         | string   | (required)            | Namespace where the controller runs                         |
| `--pod-ip`                            | string   | (required)            | Controller's pod IP for hash ring                           |
| `--kubeconfig-qps`                    | float32  | `100`                 | Kubernetes API rate limit (QPS)                             |
| `--kubeconfig-burst`                  | int      | `200`                 | Kubernetes API burst capacity                               |
| `--paseto-private-key`                | string   | (required, sensitive) | Ed25519 private key for PASETO tokens                       |
| `--heartbeat-timeout`                 | duration | `90s`                 | Time before scaling function to 0                           |
| `--scale-interval`                    | duration | `15s`                 | How often scaling calculations run                          |
| `--hpa-tolerance`                     | float64  | `0.1`                 | Usage ratio tolerance band (10%)                            |
| `--hpa-initial-readiness-delay`       | duration | `30s`                 | Ignore CPU for pods newer than this                         |
| `--hpa-downscale-stabilization`       | duration | `90s`                 | Downscale stabilization window                              |
| `--hash-ring-wait-time`               | duration | `10s`                 | Timeout waiting for hash ring to populate                   |
| `--function-namespaces`               | []string | (required)            | Namespaces where functions can be invoked (comma-separated) |
| `--function-assign-path`              | string   | `/__skipper/assign`   | HTTP endpoint on pods for assignment                        |
| `--function-assign-timeout`           | duration | `30s`                 | Timeout for assignment HTTP request                         |
| `--max-concurrent-stale-replacements` | int      | `10`                  | Max concurrent stale pod replacements (must be >= 1)        |
| `--skip-forbidden-namespaces`         | bool     | `false`               | Skip namespaces the service account lacks access to         |

## Router configuration

| Flag                                 | Type     | Default    | Description                                                                                              |
| ------------------------------------ | -------- | ---------- | -------------------------------------------------------------------------------------------------------- |
| `--host`                             | string   | `0.0.0.0`  | Hostname the router listens on                                                                           |
| `--port`                             | int      | `8080`     | HTTP server port                                                                                         |
| `--shutdown-timeout`                 | duration | `5s`       | Graceful shutdown timeout                                                                                |
| `--pod-ip`                           | string   | (required) | Router's pod IP (used in heartbeats)                                                                     |
| `--heartbeat-interval`               | duration | `5s`       | Frequency of heartbeat transmission                                                                      |
| `--max-round-trip-attempts`          | int      | `6`        | Maximum retry attempts per request                                                                       |
| `--round-trip-retry-min-timeout`     | duration | `100ms`    | Minimum backoff between retries                                                                          |
| `--round-trip-retry-max-timeout`     | duration | `5s`       | Maximum backoff between retries                                                                          |
| `--controller-service-host`          | string   | (required) | Controller service hostname                                                                              |
| `--controller-port`                  | int      | `50051`    | Controller gRPC port                                                                                     |
| `--controller-headless-service-host` | string   |            | Optional headless service for controller discovery. Falls back to `--controller-service-host` if not set |

## Shared configuration

These flags are available on both the controller and the router.

| Flag                           | Type     | Default   | Description                                |
| ------------------------------ | -------- | --------- | ------------------------------------------ |
| `--log-level`                  | string   | `info`    | Log level: trace, debug, info, warn, error |
| `--log-format`                 | string   | `json`    | Log format: json or text                   |
| `--telemetry`                  | bool     | `false`   | Enable OpenTelemetry                       |
| `--telemetry-trace`            | bool     | `true`    | Enable tracing (when telemetry enabled)    |
| `--telemetry-metric`           | bool     | `true`    | Enable metrics (when telemetry enabled)    |
| `--telemetry-shutdown-timeout` | duration | `5s`      | Telemetry shutdown timeout                 |
| `--telemetry-prometheus-host`  | string   | `0.0.0.0` | Prometheus metrics endpoint host           |
| `--telemetry-prometheus-port`  | int      | `9090`    | Prometheus metrics endpoint port           |
| `--telemetry-metric-otlp`      | bool     | `false`   | Send metrics to OTLP endpoint              |
| `--pprof`                      | bool     | `true`    | Enable pprof HTTP endpoint                 |
| `--pprof-host`                 | string   | `0.0.0.0` | Pprof endpoint host                        |
| `--pprof-port`                 | int      | `6060`    | Pprof endpoint port                        |
| `--pprof-shutdown-timeout`     | duration | `5s`      | Pprof shutdown timeout                     |

## Environment variable substitution

The pattern for deriving environment variable names from flags:

1. Uppercase the flag name
2. Replace hyphens with underscores
3. Prefix with `SKIPPER_`

For example, `--heartbeat-timeout` becomes `SKIPPER_HEARTBEAT_TIMEOUT`.

This is particularly useful in Kubernetes manifests:

```yaml
env:
  - name: SKIPPER_POD_IP
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
  - name: SKIPPER_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
```
