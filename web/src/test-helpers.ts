import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  ClusterStateSchema,
  ConfigValueSchema,
  EventSchema,
  FunctionSchema,
  HeartbeatStateSchema,
  HeartbeatSchema,
  InstanceSchema,
  ScaleSchema,
  ScaleDecisionSchema,
  SupervisorStateSchema,
} from "./gen/types_pb.ts";

export function makeTimestamp(offsetMs = 0): Timestamp {
  return timestampFromMs(Date.now() + offsetMs);
}

export function makeScale(overrides?: Record<string, unknown>) {
  return create(ScaleSchema, {
    minInstances: 1,
    maxInstances: 5,
    targetCpuUsageMilli: 500,
    targetMemoryUsageMib: 256,
    targetInFlightRequests: 10,
    ...(overrides as any),
  });
}

export function makeFunction(overrides?: Record<string, unknown>) {
  return create(FunctionSchema, {
    namespace: "default",
    deployment: "web-app",
    tenant: "tenant-1",
    metadata: "",
    ...(overrides as any),
  });
}

export function makeInstance(overrides?: Record<string, unknown>) {
  return create(InstanceSchema, {
    name: "pod-abc123",
    addr: "10.0.0.1:8080",
    replicaSet: "web-app-rs1",
    cpuUsageMilli: 100,
    memoryUsageMib: 64,
    assignedAt: makeTimestamp(-60_000),
    readyAt: makeTimestamp(-55_000),
    ...(overrides as any),
  });
}

export function makeHeartbeatState(overrides?: Record<string, unknown>) {
  return create(HeartbeatStateSchema, {
    routerIp: "10.0.1.1",
    heartbeat: create(HeartbeatSchema, {
      inFlightRequests: 5,
      timestamp: makeTimestamp(-2_000),
    }),
    ...(overrides as any),
  });
}

export function makeScaleDecision(overrides?: Record<string, unknown>) {
  return create(ScaleDecisionSchema, {
    desiredInstances: 3,
    unclampedDesiredInstances: 7,
    reason: 1,
    timestamp: makeTimestamp(-30_000),
    ...(overrides as any),
  });
}

export function makeSupervisor(overrides?: Record<string, unknown>) {
  return create(SupervisorStateSchema, {
    function: makeFunction(),
    instances: [makeInstance()],
    routerHeartbeats: [makeHeartbeatState()],
    responsibleControllerIp: "10.0.0.100",
    ...(overrides as any),
  });
}

export function makeEvent(overrides?: Record<string, unknown>) {
  return create(EventSchema, {
    timestamp: makeTimestamp(-10_000),
    function: makeFunction(),
    type: 1, // SCALE_UP
    message: "scaled up to 3",
    severity: 1, // INFO
    ...(overrides as any),
  });
}

export function makeConfigValue(overrides?: Record<string, unknown>) {
  return create(ConfigValueSchema, {
    name: "heartbeat-timeout",
    value: "90s",
    description: "How long to wait before scaling to 0",
    ...(overrides as any),
  });
}

export function makeClusterState(overrides?: Record<string, unknown>) {
  return create(ClusterStateSchema, {
    podIp: "10.0.0.100",
    startedAt: makeTimestamp(-3_600_000),
    controllerIps: ["10.0.0.100", "10.0.0.101"],
    supervisors: [makeSupervisor()],
    ...(overrides as any),
  });
}
