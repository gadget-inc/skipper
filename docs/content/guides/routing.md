---
title: Routing and Proxying
description: How Skipper routes HTTP and WebSocket traffic to assigned instances.
---

## The `X-Skipper-Assignment` header

Every request to the router must include an `X-Skipper-Assignment` header containing a JSON-encoded Assignment protobuf. The flat shape is preferred; nested `scale.*` remains accepted as the one back-compat input shape (`scale_*` flat fields take precedence when both are set).

Flat shape (preferred):

```json
{
  "namespace": "production",
  "deployment": "my-app",
  "tenant": "tenant-123",
  "metadata": "v2",
  "scale_min_instances": 1,
  "scale_max_instances": 10,
  "scale_target_cpu_millicores": 500,
  "oneshot": false
}
```

Nested-`scale` shape (still accepted):

```json
{
  "namespace": "production",
  "deployment": "my-app",
  "tenant": "tenant-123",
  "metadata": "v2",
  "scale": {
    "min_instances": 1,
    "max_instances": 10,
    "target_cpu_usage_milli": 500
  },
  "oneshot": false
}
```

The legacy `X-Skipper-Function` header is also accepted by the router. When both headers are present on the same request, `X-Skipper-Assignment` wins.

The `metadata` field is an opaque string delivered to the pod during assignment. Use it for arbitrary data your application needs at runtime (configuration values, version tags, etc.).

The router returns 400 if no assignment header is present. The only exception is `GET /healthz`, which returns 200 without requiring the header.

## Request flow

1. Router extracts the assignment from the `X-Skipper-Assignment` header (or legacy `X-Skipper-Function`).
2. Creates or updates heartbeat state for the assignment.
3. Marks the assignment as active. A shared background loop sends heartbeats for all active assignments every 5 seconds (configurable via `--heartbeat-interval`).
4. Queries the controller via gRPC `GetInstance` RPC for an instance address.
5. Sets the request URL to `http://<instance-addr>`.
6. Proxies the request to the backend pod.
7. For oneshot assignments: calls `ReleaseInstance` after the response completes.

## Retry logic

Failed requests are retried with up to 6 total attempts (configurable via `--max-round-trip-attempts`) with exponential backoff and jitter:

- Minimum backoff: 100ms
- Maximum backoff: 5s
- Only dial errors trigger retries (connection refused, host unreachable, dial timeout)
- TLS errors, response timeouts, and other non-dial failures are not retried

On each retry, the failed instance is excluded so the controller returns a different pod. The request body is preserved across retries.

## Header rewriting

When proxying to backend pods, the router rewrites headers:

| Header                 | Value                                                         |
| ---------------------- | ------------------------------------------------------------- |
| `X-Skipper-Assignment` | Removed                                                       |
| `X-Skipper-Function`   | Removed (legacy header, also stripped)                        |
| `Host`                 | Preserved from original request                               |
| `X-Forwarded-For`      | From incoming header, or `RemoteAddr` if absent               |
| `X-Forwarded-Host`     | From incoming header, or request `Host` if absent             |
| `X-Forwarded-Proto`    | From incoming header, or `https`/`http` based on TLS presence |
| `Forwarded`            | RFC 7239 format with proper IPv6 quoting                      |

## WebSocket support

WebSocket connections are proxied transparently. The standard HTTP upgrade mechanism handles the protocol switch -- no special configuration is needed.

The assignment remains marked as active for the duration of the WebSocket session, so heartbeats continue keeping the pods alive in the controller.

## HTTP transport configuration

The router's HTTP transport is tuned for proxying workloads:

{{< transportTable >}}

The transport is wrapped with OpenTelemetry instrumentation for distributed tracing.
