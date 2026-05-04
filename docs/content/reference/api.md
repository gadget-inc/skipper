---
title: API Reference
description: gRPC API reference for the Skipper controller service.
---

The controller exposes a gRPC service (`ControllerService`) on port 50051 (default). All RPCs are instrumented with OpenTelemetry.

## Service: ControllerService

### GetInstance

Get a ready instance for a function. If no ready instance exists, the controller assigns an unassigned pod.

```protobuf
rpc GetInstance(GetInstanceRequest) returns (GetInstanceResponse)
```

**Request:**

{{< messageTable GetInstanceRequest >}}

**Response:**

{{< messageTable GetInstanceResponse >}}

**Behavior:**

- For regular functions: returns an existing ready instance or assigns a new pod and scales to 1
- For oneshot functions: always assigns a fresh pod
- Excludes instances by name (useful when retrying after a failed dial)

### Heartbeat

Send heartbeat signals for active functions. Heartbeats prevent function timeout and inform scaling decisions.

```protobuf
rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse)
```

**Request:**

{{< messageTable HeartbeatRequest >}}

**Response:** Empty.

**Behavior:**

- Updates per-router heartbeat state for each function
- Forwards to other controllers in the ring (excluding those in the `forwarded_for` chain)

### Scale

Scale a function to a desired number of instances.

```protobuf
rpc Scale(ScaleRequest) returns (ScaleResponse)
```

**Request:**

{{< messageTable ScaleRequest >}}

**Response:**

{{< messageTable ScaleResponse >}}

**Behavior:**

- Delegates to the responsible controller if not local
- Returns current ready instances after applying the scale decision

### ReleaseInstance

Release (delete) a pod. Used by routers after oneshot requests complete.

```protobuf
rpc ReleaseInstance(ReleaseInstanceRequest) returns (ReleaseInstanceResponse)
```

**Request:**

{{< messageTable ReleaseInstanceRequest >}}

**Response:** Empty.

**Behavior:**

- Deletes the pod (idempotent -- returns success even if the pod does not exist)

## Types

### Function

{{< messageTable Function >}}

Identity is determined by namespace + deployment + tenant + oneshot. Metadata and scale are excluded from the hash.

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
