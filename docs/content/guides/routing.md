---
title: Routing and Proxying
description: How Skipper routes HTTP and WebSocket traffic to function instances.
---

## The `X-Skipper-Function` header

Every request to the router must include an `X-Skipper-Function` header containing a JSON-encoded Function protobuf:

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

The `metadata` field is an opaque string delivered to the pod during assignment. Use it for arbitrary data your application needs at runtime (configuration values, version tags, etc.).

The router returns 400 if the header is missing. The only exception is `GET /healthz`, which returns 200 without requiring the header.

## Request flow

1. Router extracts the function from the `X-Skipper-Function` header.
2. Creates or updates heartbeat state for the function.
3. Marks the function as active. A shared background loop sends heartbeats for all active functions every 5 seconds (configurable via `--heartbeat-interval`).
4. Queries the controller via gRPC `GetInstance` RPC for an instance address.
5. Sets the request URL to `http://<instance-addr>`.
6. Proxies the request to the backend pod.
7. For oneshot functions: calls `ReleaseInstance` after the response completes.

## Retry logic

Failed requests are retried with up to 6 total attempts (configurable via `--max-round-trip-attempts`) with exponential backoff and jitter:

- Minimum backoff: 100ms
- Maximum backoff: 5s
- Only dial errors trigger retries (connection refused, host unreachable, dial timeout)
- TLS errors, response timeouts, and other non-dial failures are not retried

On each retry, the failed instance is excluded so the controller returns a different pod. The request body is preserved across retries.

## Oneshot instance failures

When a oneshot instance drops the connection before it responds, usually because its container was OOM-killed or its pod was evicted, the router does not retry. It asks the controller why the pod stopped, waits up to the controller's `--instance-termination-wait` (default 5s) for Kubernetes to report it, and returns a `502` with a JSON body:

```json
{
  "error": "instance operation-runner-7d9c4-x2k8p stopped before it responded: container \"runner\" ran out of memory and was killed at its 6Gi limit (exit code 137)",
  "reason": "OOMKilled",
  "instance": "operation-runner-7d9c4-x2k8p",
  "container": "runner",
  "exitCode": 137,
  "memoryLimitBytes": 6442450944
}
```

<div class="table-scroll">

| Field              | Present                            | Meaning                                                                                                                                                                   |
| ------------------ | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `error`            | Always                             | One sentence saying what happened.                                                                                                                                        |
| `reason`           | Always                             | `OOMKilled`, `PodTerminated`, `ContainerExited`, or `Unknown`.                                                                                                            |
| `instance`         | Always                             | Pod name.                                                                                                                                                                 |
| `container`        | `OOMKilled`, `ContainerExited`     | Container that was killed or exited.                                                                                                                                      |
| `exitCode`         | `OOMKilled`, `ContainerExited`     | That container's exit code.                                                                                                                                               |
| `memoryLimitBytes` | `OOMKilled`, when the limit is set | The container's memory limit.                                                                                                                                             |
| `terminatedBy`     | `PodTerminated`, when known        | `HeartbeatTimeout` when skipper deleted the pod, otherwise Kubernetes' disruption reason (`EvictionByEvictionAPI`, `TerminationByKubelet`, `PreemptionByScheduler`, ...). |

</div>

`Unknown` means Kubernetes reported nothing within the wait. Other proxy errors, and failures on standard functions, return an empty `502` as before.

## Header rewriting

When proxying to backend pods, the router rewrites headers:

| Header               | Value                                                         |
| -------------------- | ------------------------------------------------------------- |
| `X-Skipper-Function` | Removed                                                       |
| `Host`               | Preserved from original request                               |
| `X-Forwarded-For`    | From incoming header, or `RemoteAddr` if absent               |
| `X-Forwarded-Host`   | From incoming header, or request `Host` if absent             |
| `X-Forwarded-Proto`  | From incoming header, or `https`/`http` based on TLS presence |
| `Forwarded`          | RFC 7239 format with proper IPv6 quoting                      |

## WebSocket support

WebSocket connections are proxied transparently. The standard HTTP upgrade mechanism handles the protocol switch -- no special configuration is needed.

The function remains marked as active for the duration of the WebSocket session, so heartbeats continue keeping the pods alive in the controller.

## HTTP transport configuration

The router's HTTP transport is tuned for proxying workloads:

{{< transportTable >}}

The transport is wrapped with OpenTelemetry instrumentation for distributed tracing.
