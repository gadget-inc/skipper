---
title: Controller Internals
description: How the controller discovers pods, supervises assignments, scales deployments, and safely binds pods.
---

The controller is the stateful core of Skipper. It watches Kubernetes resources via informers, runs a per-assignment supervisor loop, and handles pod binding with optimistic concurrency.

## Informer architecture

The controller uses Kubernetes informers (shared informer factory pattern) for event-driven pod management. All informers resync every 5 minutes.

### Informer types

1. **Controller pod informer** -- watches controller pods to maintain the hash ring. When a controller pod appears or disappears, the ring is updated.

2. **Assignment pod informers** -- per-namespace, filtered by the `skipper/deployment` label. These track the pods available for binding to assignments. A custom indexer on `assignmentHash` enables efficient lookups.

3. **ReplicaSet informers** -- per-namespace, for tracking deployment rollouts and detecting stale pods that belong to old ReplicaSets.

### Event lag measurement

Informer event lag (the time between an event occurring and the handler processing it) is measured and exposed as a histogram metric, providing visibility into controller responsiveness.

## Supervisor pattern

Each active assignment gets its own Supervisor goroutine that runs independently with its own context and cancel function. `sync.Once` ensures the converge loop starts exactly once per Supervisor.

### Converge loop

The Supervisor executes the following steps every `--scale-interval` (default: 15s):

1. Get current instances from the pod informer
2. Clear stale heartbeats if the assignment lost all instances
3. Calculate desired instances using the HPA algorithm
4. Record the recommendation to the stabilization window
5. Check responsibility via the hash ring (early return if not responsible)
6. Clean up stuck instances
7. Execute scaling (bind or delete pods)
8. Replace stale instances (only when not scaling down)

For oneshot assignments, each request gets its own pod. The supervisor only intervenes as a safety net — if a pod's heartbeat times out, the controller deletes it and stops the loop.

## Scaling internals

### Controller startup stabilization

New controllers wait `max(downscale_stabilization, heartbeat_timeout)` before executing their first downscale. This prevents a freshly started controller from immediately scaling down assignments that were recently active on another replica.

### Recommendation tracking

Each scaling recommendation is timestamped. Expired entries are pruned on every converge loop iteration. During downscale, the autoscaler uses the maximum recommendation within the stabilization window to prevent rapid oscillation.

### Missing metrics handling

When calculating averages, instances that have not reported metrics are handled differently depending on direction:

- **Scaling down**: missing instances are assumed at 100% usage (prevents premature downscale).
- **Scaling up**: missing instances are assumed at 0% usage (prevents unnecessary upscale).

For CPU metrics, pods newer than `--hpa-initial-readiness-delay` (default: 30s) are excluded to avoid counting startup spikes.

## Pod assignment safety

When a request arrives for an assignment with no ready instances, the controller binds an unassigned pod from the deployment pool through a multi-step process designed for safety under concurrent access.

### Assignment walkthrough

1. **Find an unassigned pod.** The controller polls every 250ms for a pod with no tenant label.
2. **Resolve the port.** Reads the `skipper/port` annotation if set (name or number), otherwise uses the first container port.
3. **Atomic metadata patch.** Applies a JSON patch to the pod with four test operations:
   - Pod has a ReplicaSet owner
   - Pod is not already assigned (no tenant label)
   - No `skipper/assignment` (or legacy `skipper/function`) annotation exists
   - No assigned-at annotation exists

   All four conditions must hold for the patch to succeed. If any test fails, another controller (or a previous attempt) already claimed the pod — the controller retries with a different pod (409 conflict).

4. **Send assignment token.** HTTP POST to `/__skipper/assign` on the pod with a PASETO-signed token (Ed25519, 7-day expiry).
5. **Mark ready.** On success, the controller patches the pod with a `ready-at` timestamp. On failure, it deletes the pod and retries.

This makes assignment idempotent — if two controllers try to bind the same pod simultaneously, only one succeeds.

## Caching

The controller uses caching to avoid redundant work on hot paths:

- **Pod port cache** by UID — avoids repeated annotation parsing for port resolution
- **Assignment cache** by annotation JSON — avoids repeated deserialization of `skipper/assignment` (and legacy `skipper/function`) pod annotations

## Heartbeat forwarding protocol

When a router sends heartbeats, the receiving controller:

1. Processes heartbeats locally (updates supervisor state)
2. Computes forwarding targets: ring members not in the `forwardedFor` chain
3. Forwards with an updated chain (adds its own IP)

The `forwardedFor` chain prevents cycles — each controller only forwards to ring members not already in the chain. This ensures eventual consistency across all controllers. Any controller can receive heartbeats from any router — there is no single point of failure.
