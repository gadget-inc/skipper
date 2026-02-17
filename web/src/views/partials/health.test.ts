import { describe, expect, it, vi } from "vitest";
import {
  makeClusterState,
  makeInstance,
  makeSupervisor,
  makeHeartbeatState,
  makeTimestamp,
} from "../../test-helpers.ts";
import { healthIndicators, computeHealthIssues } from "./health.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../../gen/types_pb.ts";

describe("healthIndicators", () => {
  it("shows all healthy when no issues", () => {
    const state = makeClusterState();
    const html = healthIndicators(state);
    expect(html).toContain("health-dot-green");
    expect(html).toContain("All healthy");
  });

  it("shows stuck instances in red", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const inst = makeInstance({ readyAt: undefined, assignedAt: makeTimestamp(-120_000) }); // 2 min ago, no ready
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    expect(html).toContain("health-dot-red");
    expect(html).toContain("Stuck instances");
    expect(html).toContain("badge-red");

    vi.useRealTimers();
  });

  it("shows functions waiting for pods in yellow", () => {
    const inst = makeInstance({ readyAt: undefined, assignedAt: makeTimestamp(-5_000) }); // 5s ago, not stuck yet
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    expect(html).toContain("Functions waiting for pods");
    expect(html).toContain("health-dot-yellow");
  });

  it("shows stale heartbeats in yellow", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const hb = makeHeartbeatState({
      heartbeat: create(HeartbeatSchema, {
        inFlightRequests: 5,
        timestamp: makeTimestamp(-70_000),
      }),
    });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    expect(html).toContain("Stale heartbeats");
    expect(html).toContain("health-dot-yellow");

    vi.useRealTimers();
  });

  it("shows stale instances in yellow", () => {
    const inst = makeInstance({ replicaSet: "old-rs" });
    const sup = makeSupervisor({ instances: [inst], activeReplicaSet: "new-rs" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    expect(html).toContain("Stale instances");
    expect(html).toContain("health-dot-yellow");
  });

  it("does not show stale instances when on active replica set", () => {
    const inst = makeInstance({ replicaSet: "active-rs" });
    const sup = makeSupervisor({ instances: [inst], activeReplicaSet: "active-rs" });
    const state = makeClusterState({ supervisors: [sup] });
    const issues = computeHealthIssues(state);
    const staleIssue = issues.find((i) => i.label === "Stale instances");
    expect(staleIssue).toBeUndefined();
  });
});
