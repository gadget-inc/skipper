---
title: Router Internals
description: Performance optimizations and memory management strategies in the Skipper router.
---

The router is designed for minimal per-request overhead. This page covers the caching, pooling, and memory strategies that keep allocations low under high throughput.

## Router optimizations

The router uses several strategies to minimize per-request overhead:

- **Function header LRU cache** — 4096-entry cache keyed by the raw `x-skipper-function` header value, avoiding repeated JSON unmarshalling on identical requests.
- **Body preservation** — request bodies are wrapped with `NopCloser` to allow re-reading across retries. This prevents the transport from closing the body on dial errors, keeping it readable for retries.
- **Buffer pools** — HTTP response bodies use a pool (32KB initial, 1MB max before discard). Forwarded header construction uses a separate pool (256B initial, 4KB max).
- **Atomic heartbeat state** — per-function heartbeat state uses atomic operations to minimize per-request allocations. Protobuf objects are only materialized when sending heartbeats (every 5s), not on every request. The `heartbeatState` struct tracks function pointer, in-flight count, and last-active timestamp atomically.
