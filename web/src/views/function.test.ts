import { describe, expect, it } from "vitest";
import {
  makeClusterState,
  makeInstance,
  makeSupervisor,
  makeFunction,
  makeScale,
  makeScaleDecision,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { functionPage, scalingHistorySection } from "./function.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

describe("functionPage", () => {
  const key = "default:web-app:tenant-1";

  it("shows not found for unknown key", async () => {
    const state = makeClusterState();
    const html = await functionPage(state, "bad:key:here").text();
    expect(html).toContain("Function not found");
    expect(html).toContain("bad:key:here");
  });

  it("renders namespace in summary card", async () => {
    const html = await functionPage(makeClusterState(), key).text();
    expect(html).toContain(">Namespace<");
    expect(html).toContain(">default<");
  });

  it("renders instance counts (ready/total)", async () => {
    const sup = makeSupervisor({
      instances: [makeInstance(), makeInstance({ readyAt: undefined })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain(">Instances<");
    expect(html).toContain(">1/2<");
  });

  it("renders scale range in summary card", async () => {
    const fn = makeFunction({ scale: makeScale({ minInstances: 1, maxInstances: 8 }) });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain(">Scale Range<");
    expect(html).toContain("1–8");
  });

  it("renders dash for scale range when no scale", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain(">Scale Range<");
    // Dash in the value div
    expect(html).toContain(">—<");
  });

  it("renders responsible controller IP as link", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain(">Responsible<");
    expect(html).toContain('href="/controllers/10.0.0.42"');
    expect(html).toContain(">10.0.0.42</a>");
  });

  it("renders scale config section when scale exists", async () => {
    const fn = makeFunction({
      scale: makeScale({
        targetCpuUsageMilli: 750,
        targetMemoryUsageMib: 512,
        targetInFlightRequests: 20,
      }),
    });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain("Scale Configuration");
    expect(html).toContain("750m");
    expect(html).toContain("512Mi");
    expect(html).toContain(">20<");
  });

  it("omits scale config section when no scale", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).not.toContain("Scale Configuration");
  });

  it("renders last scale decision section when present", async () => {
    const sup = makeSupervisor({
      lastScaleDecision: makeScaleDecision({
        desiredInstances: 4,
        unclampedDesiredInstances: 12,
        reason: 1,
      }),
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain("Last Scale Decision");
    expect(html).toContain(">4<");
    expect(html).toContain(">12<");
    expect(html).toContain("cpu");
  });

  it("omits last scale decision section when absent", async () => {
    const sup = makeSupervisor({ lastScaleDecision: undefined });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).not.toContain("Last Scale Decision");
  });

  it("shows empty heartbeats message when none", async () => {
    const sup = makeSupervisor({ routerHeartbeats: [] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain("No router heartbeats");
  });

  it("renders heartbeat table with router IP links and in-flight counts", async () => {
    const hb1 = makeHeartbeatState({
      routerIp: "10.0.1.1",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 3 }),
    });
    const hb2 = makeHeartbeatState({
      routerIp: "10.0.1.2",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 7 }),
    });
    const sup = makeSupervisor({ routerHeartbeats: [hb1, hb2] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain('href="/routers/10.0.1.1"');
    expect(html).toContain('href="/routers/10.0.1.2"');
    expect(html).toContain(">3<");
    expect(html).toContain(">7<");
  });

  it("includes HTMX polling for instances with URL-encoded key", async () => {
    const html = await functionPage(makeClusterState(), key).text();
    const encodedKey = encodeURIComponent(key);
    expect(html).toContain(`hx-get="/partials/instances/${encodedKey}"`);
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("uses namespace:deployment:tenant format for key matching", async () => {
    const fn = makeFunction({ namespace: "prod", deployment: "api-server", tenant: "org-42" });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });

    // Should find with correct key
    const found = await functionPage(state, "prod:api-server:org-42").text();
    expect(found).not.toContain("Function not found");
    expect(found).toContain("api-server");

    // Should not find with wrong key
    const notFound = await functionPage(state, "prod:api-server:wrong").text();
    expect(notFound).toContain("Function not found");
  });

  it("shows rollout banner when instances span multiple replica sets", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs2",
      instances: [
        makeInstance({ name: "pod-1", replicaSet: "web-app-rs1" }),
        makeInstance({ name: "pod-2", replicaSet: "web-app-rs2" }),
      ],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain("Rollout in progress");
    expect(html).toContain("1 stale");
    expect(html).toContain("1 current");
    expect(html).toContain("banner-yellow");
  });

  it("hides rollout banner when all instances are current", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs1",
      instances: [makeInstance({ replicaSet: "web-app-rs1" })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).not.toContain("Rollout in progress");
  });

  it("hides rollout banner when no activeReplicaSet", async () => {
    const sup = makeSupervisor({ activeReplicaSet: "" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).not.toContain("Rollout in progress");
  });

  it("passes activeReplicaSet to instances table", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs2",
      instances: [makeInstance({ replicaSet: "web-app-rs1" })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    expect(html).toContain(">stale<");
  });

  it("returns correct content-type", () => {
    const response = functionPage(makeClusterState(), key);
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});

describe("scalingHistorySection", () => {
  it("returns empty string when history is empty", () => {
    expect(scalingHistorySection([])).toBe("");
  });

  it("renders history table with decisions", () => {
    const decisions = [
      makeScaleDecision({ desiredInstances: 2, unclampedDesiredInstances: 2 }),
      makeScaleDecision({ desiredInstances: 5, unclampedDesiredInstances: 5 }),
    ];
    const html = scalingHistorySection(decisions);
    expect(html).toContain("Scaling History");
    expect(html).toContain(">2<");
    expect(html).toContain(">5<");
  });

  it("highlights clamped rows", () => {
    const decisions = [makeScaleDecision({ desiredInstances: 5, unclampedDesiredInstances: 12 })];
    const html = scalingHistorySection(decisions);
    expect(html).toContain("row-highlight");
    expect(html).toContain("(clamped)");
  });

  it("does not highlight unclamped rows", () => {
    const decisions = [makeScaleDecision({ desiredInstances: 3, unclampedDesiredInstances: 3 })];
    const html = scalingHistorySection(decisions);
    expect(html).not.toContain("row-highlight");
    expect(html).not.toContain("(clamped)");
  });
});
