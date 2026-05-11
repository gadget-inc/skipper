---
title: Deployments
description: How to deploy workloads that Skipper can discover, bind to assignments, and manage.
---

A deployment is a pool of pods that Skipper discovers and manages. Each pod in the pool is identical at start-up and is bound to a particular tenant only once a request for that tenant's assignment arrives. One deployment serves multiple tenants simultaneously — Skipper dynamically binds pods to assignments and tears them down again when the assignment goes idle.

## Pod discovery

Skipper discovers eligible pods by watching for the `skipper/deployment` label:

| Label                | Purpose                                                                 |
| -------------------- | ----------------------------------------------------------------------- |
| `skipper/deployment` | Identifies the deployment name. Required for Skipper to manage the pod. |

The controller only watches namespaces listed in the `--assignment-namespaces` flag (the legacy `--function-namespaces` flag is still accepted with a deprecation log). Pods in other namespaces are invisible to Skipper regardless of labels.

## Assignment identity

An assignment is uniquely identified by four fields: **namespace**, **deployment**, **tenant**, and **oneshot** flag. These are combined into a unique identity used throughout the system.

Policy fields (scale, retry, transport, etc.) and `metadata` are deliberately excluded from identity. Changing an assignment's scaling targets or metadata does not create a new identity -- the existing instances continue serving.

Callers address assignments by including these identity fields in the `X-Skipper-Assignment` HTTP header on each request to the router (the legacy `X-Skipper-Function` header is still accepted; when both are present, `X-Skipper-Assignment` wins). See [Routing and Proxying](/skipper/guides/routing/) for the full header format and request flow.

## Pod assignment

When a request arrives for an assignment with no ready instances, the controller binds an unassigned pod from the deployment pool.

### Binding process

The controller finds an unassigned pod, claims it atomically, sends a PASETO token, and marks it ready. If the bind fails, it retries with a different pod.

1. **Find an unassigned pod** — waits for a pod with no tenant label, checking every 250ms until one is available.
2. **Resolve the port** — reads the `skipper/port` annotation if set, otherwise uses the first container port.
3. **Claim the pod** — applies an atomic patch that fails if the pod is already claimed by another controller.
4. **Send assignment token** — HTTP POST to `/__skipper/assign` on the pod with a PASETO-signed token (7-day expiry by default; tenant-overridable via `assign_token_ttl`).
5. **Mark ready** — on success, patches the pod with a `ready-at` timestamp. On failure, deletes the pod and retries.

## Pod annotations

Skipper uses annotations in the `skipper/` namespace to track pod state:

| Annotation            | Purpose                                                                |
| --------------------- | ---------------------------------------------------------------------- |
| `skipper/assignment`  | JSON-encoded Assignment protobuf (the new canonical key)               |
| `skipper/function`    | Same JSON, dual-written for back-compat with older controller binaries |
| `skipper/tenant`      | Tenant identifier (set during binding)                                 |
| `skipper/assigned-at` | RFC 3339 timestamp of binding                                          |
| `skipper/ready-at`    | RFC 3339 timestamp when the pod confirmed assignment                   |
| `skipper/replica-set` | ReplicaSet name (copied from owner reference)                          |
| `skipper/port`        | Optional port override (name or number)                                |

The controller reads `skipper/assignment` first and falls back to `skipper/function` when only the legacy key is present, so mid-rollout clusters interoperate in both directions.

## PASETO tokens

The controller signs assignment tokens using a private key provided via the `--paseto-private-key` flag. Tokens expire after 7 days by default; tenants can override per-assignment via the `assign_token_ttl` policy field.

During binding, the token is sent to the pod via HTTP POST to the assignment endpoint. Pods should validate the token signature to confirm the request came from a legitimate controller. The pod can show this token to anyone with the controller's public key — including the original caller — to prove it was bound by a real controller.

## Oneshot assignments

Oneshot assignments are single-use: one request per pod. Set `oneshot: true` in the assignment header to enable this mode.

Behavior differences from standard assignments:

- Scale 1:1 with in-flight requests (each request gets its own pod)
- After the request completes, the router calls `ReleaseInstance` to delete the pod
- If the heartbeat times out before the request completes, the controller deletes the orphaned pod

## Stale instance replacement

The controller detects stale instances when a replica set is scaled to zero or assignment configuration changes. To maintain availability during rollouts, it binds a replacement pod before deleting the stale one.

Concurrent replacements are limited by `--max-concurrent-stale-replacements` (default: 10). This prevents a large rollout from overwhelming the cluster with simultaneous re-bindings.

Stuck instances -- pods that were bound but never became ready after 2x the assignment timeout -- are cleaned up automatically.

## Assignment configuration

The `Assignment` message carries flat policy fields under six concern prefixes: `scale_*`, `zone_*`, `assign_*`, `heartbeat_*`, `retry_*`, `transport_*`. The full field list, including which knobs are wired and which are planned for follow-up releases, is documented in the [API Reference](/skipper/reference/api/#assignment).

Scale parameters control autoscaling behavior per assignment:

{{< messageTable Scale >}}

The autoscaler evaluates each metric independently and takes the highest recommendation, similar to the Kubernetes HPA algorithm. Instances are never scaled below `min_instances` or above `max_instances`.
