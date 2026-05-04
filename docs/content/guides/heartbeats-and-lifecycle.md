---
title: Heartbeats and Lifecycle
description: How heartbeats keep functions alive and how pod lifecycle is managed.
---

## Heartbeat mechanism

Routers send heartbeats to the controller every 5 seconds (configurable via `--heartbeat-interval` on the router). Heartbeats are batched: one `HeartbeatRequest` contains all active functions for that router. Each heartbeat includes the function identity, a timestamp, and the current in-flight request count.

The controller updates per-router heartbeat state for each function it receives.

## Heartbeat state (router side)

The router tracks per-function heartbeat state. If a function has not been active in 3 heartbeat intervals (15s by default), its state is cleaned up and removed from the heartbeat batch.

## Heartbeat forwarding (controller side)

Controllers forward heartbeats to each other so all replicas have up-to-date state. This ensures any controller can receive heartbeats from any router — there is no single point of failure.

## Heartbeat timeout

Default: 90 seconds (configurable via `--heartbeat-timeout` on the controller).

If no heartbeat is received for a function within the timeout, the controller scales the function to 0 by deleting all its pods. Scale reason: `SCALE_REASON_HEARTBEAT_TIMEOUT`.

For oneshot functions, the controller waits until it has been running long enough before cleaning up orphans. This prevents a restarting controller from immediately deleting pods that were legitimately in use.

## Pod lifecycle states

1. **Unassigned** -- pod exists with the `skipper/deployment` label but no tenant label. Available in the assignment pool.
2. **Assigned** -- pod has been patched with function metadata and a tenant label. The assignment HTTP request to the pod is in progress.
3. **Ready** -- pod confirmed assignment (HTTP 200 to `/__skipper/assign`). Has a `ready-at` annotation and is serving traffic.
4. **Terminating** -- pod is being deleted (`DeletionTimestamp` is set).

## Stuck instance cleanup

Pods stuck in the "assigned" state (never became ready) for longer than 2x the assignment timeout are deleted automatically. This handles cases where the assignment HTTP request failed silently or the pod crashed during assignment.

## Instance release (oneshot)

After a oneshot request completes, the router calls the `ReleaseInstance` RPC. This is fire-and-forget with a 5-second timeout using a background context. The controller deletes the pod. The operation is idempotent -- it returns success even if the pod is already gone.

## Function lifecycle

Each function is managed independently:

- Tracking starts when a function is first seen (via a request or pod discovery).
- Scaling is re-evaluated every scale interval (default: 15s).
- For oneshot functions: tracking stops after scaling to 0.
