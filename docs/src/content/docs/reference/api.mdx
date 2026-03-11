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

```protobuf
message Function {
  string namespace = 1;
  string deployment = 2;
  string tenant = 3;
  string metadata = 4;
  Scale scale = 5;
  bool oneshot = 6;
}
```

Identity is determined by namespace + deployment + tenant + oneshot. Metadata and scale are excluded from the hash.

### Scale

```protobuf
message Scale {
  uint32 min_instances = 1;
  uint32 max_instances = 2;
  uint32 target_cpu_usage_milli = 3;
  uint32 target_memory_usage_mib = 4;
  uint32 target_in_flight_requests = 5;
}
```

### Instance

```protobuf
message Instance {
  Function function = 1;
  string name = 2;
  string addr = 3;
  string replica_set = 4;
  google.protobuf.Timestamp assigned_at = 5;
  google.protobuf.Timestamp ready_at = 6;
  uint32 cpu_usage_milli = 7;
  uint32 memory_usage_mib = 8;
}
```

### Heartbeat

```protobuf
message Heartbeat {
  Function function = 1;
  google.protobuf.Timestamp timestamp = 2;
  uint32 in_flight_requests = 3;
}
```

### ScaleReason

| Value                             | Number | Description                     |
| --------------------------------- | ------ | ------------------------------- |
| `SCALE_REASON_UNSPECIFIED`        | 0      | Default/unknown                 |
| `SCALE_REASON_CPU`                | 1      | CPU usage triggered scaling     |
| `SCALE_REASON_HEARTBEAT_TIMEOUT`  | 2      | No heartbeat within timeout     |
| `SCALE_REASON_IN_FLIGHT_REQUESTS` | 3      | Request count triggered scaling |
| `SCALE_REASON_MEMORY`             | 4      | Memory usage triggered scaling  |
| `SCALE_REASON_NO_READY_INSTANCES` | 5      | No ready instances available    |

## Client configuration

Default retry policies for gRPC clients:

| Method          | Max Retries | Max Backoff | Retry On    |
| --------------- | ----------- | ----------- | ----------- |
| GetInstance     | 3           | 100ms       | UNAVAILABLE |
| Heartbeat       | 2           | 50ms        | UNAVAILABLE |
| Scale           | 3           | 500ms       | UNAVAILABLE |
| ReleaseInstance | 2           | 50ms        | UNAVAILABLE |

Connection uses the `dns:///` scheme with round-robin load balancing for headless service discovery.
