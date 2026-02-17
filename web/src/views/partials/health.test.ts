import { describe, it, mock } from "node:test";
import assert from "node:assert/strict";
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

await describe("healthIndicators", async () => {
  await it("shows all healthy when no issues", () => {
    const state = makeClusterState();
    const html = healthIndicators(state);
    assert.ok(html.includes("health-dot-green"));
    assert.ok(html.includes("All healthy"));
  });

  await it("shows stuck instances in red", () => {
    mock.timers.enable({ apis: ["Date"] });
    mock.timers.setTime(Date.now());

    const inst = makeInstance({ readyAt: undefined, assignedAt: makeTimestamp(-120_000) }); // 2 min ago, no ready
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    assert.ok(html.includes("health-dot-red"));
    assert.ok(html.includes("Stuck instances"));
    assert.ok(html.includes("badge-red"));

    mock.timers.reset();
  });

  await it("shows functions waiting for pods in yellow", () => {
    const inst = makeInstance({ readyAt: undefined, assignedAt: makeTimestamp(-5_000) }); // 5s ago, not stuck yet
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    assert.ok(html.includes("Functions waiting for pods"));
    assert.ok(html.includes("health-dot-yellow"));
  });

  await it("shows stale heartbeats in yellow", () => {
    mock.timers.enable({ apis: ["Date"] });
    mock.timers.setTime(Date.now());

    const hb = makeHeartbeatState({
      heartbeat: create(HeartbeatSchema, {
        inFlightRequests: 5,
        timestamp: makeTimestamp(-70_000),
      }),
    });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    assert.ok(html.includes("Stale heartbeats"));
    assert.ok(html.includes("health-dot-yellow"));

    mock.timers.reset();
  });

  await it("shows stale instances in yellow", () => {
    const inst = makeInstance({ replicaSet: "old-rs" });
    const sup = makeSupervisor({ instances: [inst], activeReplicaSet: "new-rs" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = healthIndicators(state);
    assert.ok(html.includes("Stale instances"));
    assert.ok(html.includes("health-dot-yellow"));
  });

  await it("does not show stale instances when on active replica set", () => {
    const inst = makeInstance({ replicaSet: "active-rs" });
    const sup = makeSupervisor({ instances: [inst], activeReplicaSet: "active-rs" });
    const state = makeClusterState({ supervisors: [sup] });
    const issues = computeHealthIssues(state);
    const staleIssue = issues.find((i) => i.label === "Stale instances");
    assert.equal(staleIssue, undefined);
  });
});
