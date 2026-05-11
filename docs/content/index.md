---
title: Introduction
description: A Kubernetes controller that turns deployments into a pool of assignments servable to tenants, managing discovery, assignment, scaling, routing, and termination.
---

You run a multi-tenant platform. Each tenant needs their own isolated server running your application. You could manage that yourself — spinning up servers when tenants arrive, tearing them down when they leave, scaling under load, routing traffic to the right place — but that's a lot of moving parts to get right.

Skipper handles all of it automatically.

## What Skipper does

Skipper maintains a pool of servers. When a tenant needs one, Skipper assigns a server from the pool, routes traffic to it, and scales capacity based on demand. When a tenant disconnects and stops sending requests, Skipper shuts down the idle server so capacity is available for future demand.

The key ideas:

- **On-demand assignment** — servers are assigned to tenants only when requests arrive, not pre-provisioned
- **Automatic scaling** — Skipper scales servers up and down based on CPU, memory, and request load
- **Traffic routing** — incoming requests are matched to the right server without any manual wiring
- **Idle reclamation** — servers that stop receiving traffic are terminated and returned to the pool

No tenant gets a permanently reserved server. No server sits idle burning resources. Skipper keeps supply matched to demand.

## How it works under the hood

Skipper runs on Kubernetes and is split into two components that communicate over gRPC.

**Controller** watches for deployments labeled with `skipper/deployment`, manages pod lifecycle (creation, assignment, scaling, termination), and exposes a gRPC API for instance lookups. It coordinates across replicas for high availability.

**Router** sits in the request path. It receives HTTP traffic, resolves the target assignment from the `x-skipper-assignment` header (or legacy `x-skipper-function`), queries the Controller for a bound instance, and proxies the request to the pod. It handles WebSocket upgrades, retries failed requests, and sends heartbeats to keep assigned pods alive during active connections.

## Request flow

1. A client sends an HTTP request to the Router with an `x-skipper-assignment` header identifying the target assignment.
2. The Router queries a Controller replica (via DNS round-robin) for an instance bound to that assignment.
3. The Controller looks up or binds a pod from the deployment pool, signs a PASETO token binding the pod to the tenant, and returns the instance address.
4. The Router proxies the request to the assigned pod. For long-lived connections (WebSocket, streaming), it sends periodic heartbeats to the Controller.
5. When heartbeats stop (default timeout: 90s), the Controller marks the assignment as inactive and terminates the pod, returning it to the pool.

## What's next

- [Deployments](/skipper/guides/deployments/) — label your deployments and configure pool behavior
- [Routing](/skipper/guides/routing/) — request resolution, proxying, and WebSocket support
- [Scaling](/skipper/guides/scaling/) — autoscaling behavior, metrics, and tuning
- [Observability](/skipper/guides/observability/) — tracing, metrics, and debugging
- [Architecture overview](/skipper/architecture/overview/) — how the Controller and Router fit together
- [Contributing](/skipper/contributing/getting-started/) — set up a development environment
