---
title: API Reference
description: gRPC API reference for the Skipper controller service.
---

The controller exposes a gRPC service (`ControllerService`) on port 50051 (default). All RPCs are instrumented with OpenTelemetry.

## Service: ControllerService

### GetInstance

Get a ready instance for an assignment. If no ready instance exists, the controller binds an unassigned pod.

{{< serviceMethod GetInstance >}}

**Request:**

{{< messageTable GetInstanceRequest >}}

**Response:**

{{< messageTable GetInstanceResponse >}}

**Behavior:**

- For regular assignments: returns an existing ready instance or binds a new pod and scales to 1
- For oneshot assignments: always binds a fresh pod
- Excludes instances by name (useful when retrying after a failed dial)

### Heartbeat

Send heartbeat signals for active assignments. Heartbeats prevent assignment timeout and inform scaling decisions.

{{< serviceMethod Heartbeat >}}

**Request:**

{{< messageTable HeartbeatRequest >}}

**Response:** Empty.

**Behavior:**

- Updates per-router heartbeat state for each assignment
- Forwards to other controllers in the ring (excluding those in the `forwarded_for` chain)

### Scale

Scale an assignment to a desired number of instances.

{{< serviceMethod Scale >}}

**Request:**

{{< messageTable ScaleRequest >}}

**Response:**

{{< messageTable ScaleResponse >}}

**Behavior:**

- Delegates to the responsible controller if not local
- Returns current ready instances after applying the scale decision

### ReleaseInstance

Release (delete) a pod. Used by routers after oneshot requests complete.

{{< serviceMethod ReleaseInstance >}}

**Request:**

{{< messageTable ReleaseInstanceRequest >}}

**Response:** Empty.

**Behavior:**

- Deletes the pod (idempotent -- returns success even if the pod does not exist)

### GetClusterState

Return a snapshot of the cluster's current state -- supervisors, recent events, and active configuration. Used by the web UI and operator tooling.

{{< serviceMethod GetClusterState >}}

**Request:** Empty.

**Response:**

{{< messageTable GetClusterStateResponse >}}

**Behavior:**

- Aggregates state from every controller in the ring; each controller contributes the supervisors it owns

## Types

### Assignment

{{< messageTable Assignment >}}

Identity is determined by namespace + deployment + tenant + oneshot. Metadata and policy fields (scale, retry, transport, etc.) are excluded from the hash.

### Scale

{{< messageTable Scale >}}

### Instance

{{< messageTable Instance >}}

### Heartbeat

{{< messageTable Heartbeat >}}

### ScaleReason

{{< scaleReasonTable >}}

## Client configuration

Default retry policies for gRPC clients:

{{< grpcRetryTable >}}

Connection uses the `dns:///` scheme with round-robin load balancing for headless service discovery.
