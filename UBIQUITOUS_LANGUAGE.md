# Ubiquitous Language

## Core domain entities

| Term            | Definition                                                                                     | Aliases to avoid              |
| --------------- | ---------------------------------------------------------------------------------------------- | ----------------------------- |
| **Skipper**     | The system that turns Kubernetes deployments into a pool of assignments servable to tenants.   | —                             |
| **Assignment**  | A tenant's slice of a Kubernetes deployment with its scaling, retry, and routing policy.       | Function                      |
| **Tenant**      | The consumer who configures an assignment by sending the `x-skipper-assignment` header.        | Customer, client              |
| **Deployment**  | A Kubernetes deployment carrying the `skipper/deployment` label, the pool feeding assignments. | App, service                  |
| **Pod**         | A Kubernetes pod from a deployment, in either `Unassigned` or `Assigned` state.                | Container, replica            |
| **Instance**    | An assigned pod, addressed by IP + port, carrying its zone and resource-usage metrics.         | Worker, server, backend       |
| **Metadata**    | An opaque tenant-supplied string (often JSON) Skipper passes through without validation.       | Annotation, payload, userdata |

## Components

| Term           | Definition                                                                                                       | Aliases to avoid           |
| -------------- | ---------------------------------------------------------------------------------------------------------------- | -------------------------- |
| **Controller** | Skipper component that watches pods, runs the converge tick, and performs the assignment handshake.              | Manager, scheduler         |
| **Router**     | Skipper component that proxies tenant requests to instances and reports heartbeats to the controller.            | Proxy, gateway             |
| **Supervisor** | Per-assignment goroutine inside the controller that runs the scaling loop and holds the assignment snapshot.     | Manager, worker            |

## Policy concerns

The `Assignment` carries flat policy fields under six concern prefixes. Each concern is a cohesive thing tenants tune together.

| Concern        | Definition                                                                                                                  | Aliases to avoid                              |
| -------------- | --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| **Scale**      | HPA-style scaling: instance bounds, resource targets, tolerance, downscale stabilization, readiness delay.                  | Autoscaling, HPA policy                       |
| **Zone**       | Bi-component zone awareness: controller-side placement spread, router-side instance affinity.                               | Topology, region, locality                    |
| **Assign**     | Controller→pod handshake configuration: callback path, timeout, and PASETO token TTL.                                       | Handshake, lifecycle                          |
| **Heartbeat**  | Liveness protocol configuration: router→controller ping interval and controller-side scale-to-zero timeout.                 | Liveness probe, keepalive                     |
| **Retry**      | Router request-retry configuration: max attempts, backoff bounds, retryable status codes, and backpressure mode.            | Proxy policy, retries                         |
| **Transport**  | Router HTTP transport configuration: dial timeout, keepalive, idle conns, TLS handshake, HTTP/2, compression, flush.        | Connection settings, HTTP client tuning       |

## Runtime verbs and states

| Term                   | Definition                                                                                                                            | Aliases to avoid                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| **Assign (a pod)**     | Runtime act of binding a pod to an assignment: POST to `assign_path`, mint PASETO token, mark pod ready.                              | Provision, claim, allocate                              |
| **Amend (an assignment)** | Update an assignment's policy via a new header value; identity-affecting fields excluded; last-writer-wins on the same identity.   | Update, modify, mutate                                  |
| **Resolve (a knob)**   | Read a per-assignment field; if unset, return the cluster-default flag value (proto3 `Has*()` distinguishes unset from explicit zero). | Lookup, fetch                                           |
| **Backpressure**       | An instance signaling overload via a configured response code (`529`), prompting the router to retry per `retry_backpressure` mode. | Throttling, rate limiting                               |
| **Eject (an instance)** | Add an instance to the next attempt's exclusion list when `retry_backpressure: RETRY_AND_EJECT` fires.                              | Drop, blacklist, evict                                  |
| **Unassigned pod**     | A scheduled pod that has not yet been bound to an assignment.                                                                         | Free pod, available pod, unprovisioned pod              |
| **Assigned pod**       | A pod bound to an assignment via a successful handshake; carries the `skipper/assignment` annotation.                                 | Claimed pod, provisioned pod                            |
| **Stale instance**     | An instance whose underlying pod no longer matches the current assignment (e.g. deployment changed mid-flight).                        | Outdated, expired                                       |
| **Converge tick**      | One iteration of the controller's per-assignment scaling loop, reading from a single `*Assignment` snapshot.                          | Scaling cycle, reconcile loop                           |

## Relationships

- An **Assignment** is identified by `(namespace, deployment, tenant, oneshot)`; everything else is policy.
- An **Assignment** has zero-or-more **Instances**; each instance came from one **Pod** in the named **Deployment**.
- A **Tenant** can hold multiple **Assignments** (one per deployment × oneshot).
- A **Controller** runs one **Supervisor** per active **Assignment**.
- A **Pod** transitions `Unassigned` → `Assigned` exactly once, via the **assign** handshake.
- A **Heartbeat** (the wire envelope) carries an **Assignment** identity, a timestamp, and an in-flight-request count from one **Router** to the **Controller**.
- A **Backpressure** signal comes from an **Instance**; the **Router** reacts per the assignment's `retry_backpressure` mode.

## Example dialogue

> **Dev:** "When a **Tenant** sends a request with `x-skipper-assignment`, what happens if no **Instance** is available?"
> **Domain expert:** "The **Router** asks the **Controller** for an **Instance**. The **Controller**'s **Supervisor** for that **Assignment** picks an **Unassigned pod**, performs the **assign** handshake, and returns an **Assigned pod** as an **Instance**."
> **Dev:** "And if the pool is empty?"
> **Domain expert:** "The **Supervisor**'s **converge tick** scales the **Deployment** up. Once a new pod is ready, the next request triggers another **assign**."
> **Dev:** "What if a **Tenant** **amends** an **Assignment** mid-traffic — say, changes `scale_min_instances`?"
> **Domain expert:** "The next request carries the new header. The **Supervisor** swaps its **Assignment** snapshot via CAS, and the next **converge tick** uses the new value. Existing **Instances** are not re-assigned — only policy that's read at decision time changes."
> **Dev:** "And if an **Instance** sends back a `529`?"
> **Domain expert:** "That's a **Backpressure** signal. If the **Tenant** has `retry_backpressure: RETRY_AND_EJECT`, the **Router** **ejects** that instance from the next attempt and tries another one in the pool."

## Flagged ambiguities

- **"Function" is legacy.** It refers to what's now called an **Assignment**. The old `Function` proto, `x-skipper-function` header, `function_*` metric labels, `skipper/function` annotation, and FaaS framing were misleading because Skipper does not invoke code — it routes traffic to tenant-supplied pods. New code uses **Assignment** exclusively.
- **"assign" the verb vs. Assignment the type.** Both senses coexist and English handles the overlap: the verb (`assignPod`, "assign a pod") is the runtime act of binding a pod to an **Assignment**; the noun **Assignment** is the tenant's policy + identity record. Capitalization and context disambiguate. Avoid prose like "the assignment was assigned" — say "the pod was assigned to its assignment" or rewrite.
- **Kubernetes "assigned to a node" is unrelated.** The kube-scheduler concept of a pod being assigned to a node is K8s vocabulary; Skipper's "assign" means pod-to-**Assignment**, not pod-to-node. Both senses coexist in any operator's day.
- **"Heartbeat" the message vs. heartbeat the policy concern.** The wire-level `Heartbeat` proto envelope (router→controller report) keeps its name. The per-assignment policy fields `heartbeat_interval` and `heartbeat_timeout` are policy configuration, not messages. Both senses coexist; the message is always capitalized in code.
- **"assignment" lowercase as a domain noun.** When written in prose, lowercase `assignment` always refers to the **Assignment** type. The runtime verb is **assign**.
- **Metadata is not identity.** It is excluded from `AssignmentHash` and never affects routing or supervisor identity. Tenants who change `metadata` between requests do not fork their pool.
