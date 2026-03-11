---
title: Deploying Functions
description: How to deploy workloads that Skipper can discover, assign, and manage.
---

A function is a unit of work backed by a Kubernetes deployment that Skipper discovers and manages. Deployments are pooled — when a tenant needs a function, Skipper dynamically assigns a pod from the pool, so one deployment can serve multiple tenants simultaneously. Callers route requests to functions by including the `X-Skipper-Function` HTTP header.

## Function discovery

Skipper discovers functions by watching pods with a specific label.

For a pod to be discovered, it needs the `skipper/deployment` label:

| Label                | Purpose                                                                 |
| -------------------- | ----------------------------------------------------------------------- |
| `skipper/deployment` | Identifies the deployment name. Required for Skipper to manage the pod. |

The controller only watches namespaces listed in the `--function-namespaces` flag. Pods in other namespaces are invisible to Skipper regardless of labels.

## Function identity

A function is uniquely identified by four fields: **namespace**, **deployment**, **tenant**, and **oneshot** flag. These are combined into a unique identity used throughout the system.

Metadata and scale configuration are deliberately excluded from the identity. Changing a function's scaling targets or metadata does not create a new identity -- the existing instances continue serving.

Callers address functions by including these identity fields in the `X-Skipper-Function` HTTP header on each request to the router. See [Routing and Proxying](/skipper/guides/routing/) for the full header format and request flow.

## Pod assignment

When a request arrives for a function with no ready instances, the controller assigns an unassigned pod from the deployment pool.

### Assignment process

The controller finds an unassigned pod, claims it atomically, sends a PASETO assignment token, and marks it ready. If assignment fails, it retries with a different pod.

1. **Find an unassigned pod** — waits for a pod with no tenant label, checking every 250ms until one is available.
2. **Resolve the port** — reads the `skipper/port` annotation if set, otherwise uses the first container port.
3. **Claim the pod** — applies an atomic patch that fails if the pod is already assigned by another controller.
4. **Send assignment token** — HTTP POST to `/__skipper/assign` on the pod with a PASETO-signed token (7-day expiry).
5. **Mark ready** — on success, patches the pod with a `ready-at` timestamp. On failure, deletes the pod and retries.

## Pod annotations

Skipper uses annotations in the `skipper/` namespace to track pod state:

| Annotation            | Purpose                                              |
| --------------------- | ---------------------------------------------------- |
| `skipper/function`    | JSON-encoded Function protobuf                       |
| `skipper/tenant`      | Tenant identifier (set during assignment)            |
| `skipper/assigned-at` | RFC 3339 timestamp of assignment                     |
| `skipper/ready-at`    | RFC 3339 timestamp when the pod confirmed assignment |
| `skipper/replica-set` | ReplicaSet name (copied from owner reference)        |
| `skipper/port`        | Optional port override (name or number)              |

## PASETO tokens

The controller signs assignment tokens using a private key provided via the `--paseto-private-key` flag. Tokens expire after 7 days.

During assignment, the token is sent to the pod via HTTP POST to the assignment endpoint. Pods should validate the token signature to confirm the assignment came from a legitimate controller. The pod can show this token to anyone with the controller's public key — including the original caller — to prove it was assigned by a real controller.

## Oneshot functions

Oneshot functions are single-use: one request per pod. Set `oneshot: true` on the function to enable this mode.

Behavior differences from standard functions:

- Scale 1:1 with in-flight requests (each request gets its own pod)
- After the request completes, the router calls `ReleaseInstance` to delete the pod
- If the heartbeat times out before the request completes, the controller deletes the orphaned pod

## Stale instance replacement

The controller detects stale instances when a replica set is scaled to zero or function configuration changes. To maintain availability during rollouts, it assigns a replacement pod before deleting the stale one.

Concurrent replacements are limited by `--max-concurrent-stale-replacements` (default: 10). This prevents a large rollout from overwhelming the cluster with simultaneous reassignments.

Stuck instances -- pods that were assigned but never became ready after 2x the assignment timeout -- are cleaned up automatically.

## Function configuration

Scale parameters control autoscaling behavior per function:

| Field                       | Description                                        |
| --------------------------- | -------------------------------------------------- |
| `min_instances`             | Minimum instances to maintain (floor)              |
| `max_instances`             | Maximum instances allowed (ceiling)                |
| `target_cpu_usage_milli`    | Target CPU in millicores for autoscaling decisions |
| `target_memory_usage_mib`   | Target memory in MiB for autoscaling decisions     |
| `target_in_flight_requests` | Target in-flight requests per instance             |

The autoscaler evaluates each metric independently and takes the highest recommendation, similar to the Kubernetes HPA algorithm. Instances are never scaled below `min_instances` or above `max_instances`.
