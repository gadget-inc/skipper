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

| Field                    | Type            | Description                                     |
| ------------------------ | --------------- | ----------------------------------------------- |
| `function`               | Function        | The function to get an instance for (required)  |
| `exclude_instance_names` | repeated string | Instance names to exclude (for retry scenarios) |

**Response:**

| Field      | Type     | Description                  |
| ---------- | -------- | ---------------------------- |
| `instance` | Instance | The assigned, ready instance |

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

| Field           | Type               | Description                                                       |
| --------------- | ------------------ | ----------------------------------------------------------------- |
| `router_ip`     | string             | IP of the sending router                                          |
| `heartbeats`    | repeated Heartbeat | Heartbeat for each active function                                |
| `forwarded_for` | repeated string    | IPs that have already processed this heartbeat (cycle prevention) |

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

| Field               | Type        | Description                    |
| ------------------- | ----------- | ------------------------------ |
| `function`          | Function    | The function to scale          |
| `desired_instances` | uint32      | Target instance count          |
| `reason`            | ScaleReason | Why scaling is being triggered |

**Response:**

| Field       | Type              | Description                           |
| ----------- | ----------------- | ------------------------------------- |
| `instances` | repeated Instance | Current ready instances after scaling |

**Behavior:**

- Delegates to the responsible controller if not local
- Returns current ready instances after applying the scale decision

### ReleaseInstance

Release (delete) a pod. Used by routers after oneshot requests complete.

```protobuf
rpc ReleaseInstance(ReleaseInstanceRequest) returns (ReleaseInstanceResponse)
```

**Request:**

| Field      | Type     | Description             |
| ---------- | -------- | ----------------------- |
| `instance` | Instance | The instance to release |

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
