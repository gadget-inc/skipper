import { describe, it } from "node:test";
import assert from "node:assert/strict";
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

await describe("functionPage", async () => {
  const key = "default:web-app:tenant-1";

  await it("shows not found for unknown key", async () => {
    const state = makeClusterState();
    const html = await functionPage(state, "bad:key:here").text();
    assert.ok(html.includes("Function not found"));
    assert.ok(html.includes("bad:key:here"));
  });

  await it("renders namespace in summary card", async () => {
    const html = await functionPage(makeClusterState(), key).text();
    assert.ok(html.includes(">Namespace<"));
    assert.ok(html.includes(">default<"));
  });

  await it("renders instance counts (ready/total)", async () => {
    const sup = makeSupervisor({
      instances: [makeInstance(), makeInstance({ readyAt: undefined })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes(">Instances<"));
    assert.ok(html.includes(">1/2<"));
  });

  await it("renders scale range in summary card", async () => {
    const fn = makeFunction({ scale: makeScale({ minInstances: 1, maxInstances: 8 }) });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes(">Scale Range<"));
    assert.ok(html.includes("1–8"));
  });

  await it("renders dash for scale range when no scale", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes(">Scale Range<"));
    // Dash in the value div
    assert.ok(html.includes(">—<"));
  });

  await it("renders responsible controller IP as link", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes(">Responsible<"));
    assert.ok(html.includes('href="/controllers/10.0.0.42"'));
    assert.ok(html.includes(">10.0.0.42</a>"));
  });

  await it("renders scale config section when scale exists", async () => {
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
    assert.ok(html.includes("Scale Configuration"));
    assert.ok(html.includes("750m"));
    assert.ok(html.includes("512Mi"));
    assert.ok(html.includes(">20<"));
  });

  await it("omits scale config section when no scale", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(!html.includes("Scale Configuration"));
  });

  await it("renders last scale decision section when present", async () => {
    const sup = makeSupervisor({
      lastScaleDecision: makeScaleDecision({
        desiredInstances: 4,
        unclampedDesiredInstances: 12,
        reason: 1,
      }),
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes("Last Scale Decision"));
    assert.ok(html.includes(">4<"));
    assert.ok(html.includes(">12<"));
    assert.ok(html.includes("cpu"));
  });

  await it("omits last scale decision section when absent", async () => {
    const sup = makeSupervisor({ lastScaleDecision: undefined });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(!html.includes("Last Scale Decision"));
  });

  await it("shows empty heartbeats message when none", async () => {
    const sup = makeSupervisor({ routerHeartbeats: [] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes("No router heartbeats"));
  });

  await it("renders heartbeat table with router IP links and in-flight counts", async () => {
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
    assert.ok(html.includes('href="/routers/10.0.1.1"'));
    assert.ok(html.includes('href="/routers/10.0.1.2"'));
    assert.ok(html.includes(">3<"));
    assert.ok(html.includes(">7<"));
  });

  await it("includes HTMX polling for instances with URL-encoded key", async () => {
    const html = await functionPage(makeClusterState(), key).text();
    const encodedKey = encodeURIComponent(key);
    assert.ok(html.includes(`hx-get="/partials/instances/${encodedKey}"`));
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("uses namespace:deployment:tenant format for key matching", async () => {
    const fn = makeFunction({ namespace: "prod", deployment: "api-server", tenant: "org-42" });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });

    // Should find with correct key
    const found = await functionPage(state, "prod:api-server:org-42").text();
    assert.ok(!found.includes("Function not found"));
    assert.ok(found.includes("api-server"));

    // Should not find with wrong key
    const notFound = await functionPage(state, "prod:api-server:wrong").text();
    assert.ok(notFound.includes("Function not found"));
  });

  await it("shows rollout banner when instances span multiple replica sets", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs2",
      instances: [
        makeInstance({ name: "pod-1", replicaSet: "web-app-rs1" }),
        makeInstance({ name: "pod-2", replicaSet: "web-app-rs2" }),
      ],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes("Rollout in progress"));
    assert.ok(html.includes("1 stale"));
    assert.ok(html.includes("1 current"));
    assert.ok(html.includes("banner-yellow"));
  });

  await it("hides rollout banner when all instances are current", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs1",
      instances: [makeInstance({ replicaSet: "web-app-rs1" })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(!html.includes("Rollout in progress"));
  });

  await it("hides rollout banner when no activeReplicaSet", async () => {
    const sup = makeSupervisor({ activeReplicaSet: "" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(!html.includes("Rollout in progress"));
  });

  await it("passes activeReplicaSet to instances table", async () => {
    const sup = makeSupervisor({
      activeReplicaSet: "web-app-rs2",
      instances: [makeInstance({ replicaSet: "web-app-rs1" })],
    });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionPage(state, key).text();
    assert.ok(html.includes(">stale<"));
  });

  await it("returns correct content-type", () => {
    const response = functionPage(makeClusterState(), key);
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});

await describe("scalingHistorySection", async () => {
  await it("returns empty string when history is empty", () => {
    assert.equal(scalingHistorySection([]), "");
  });

  await it("renders history table with decisions", () => {
    const decisions = [
      makeScaleDecision({ desiredInstances: 2, unclampedDesiredInstances: 2 }),
      makeScaleDecision({ desiredInstances: 5, unclampedDesiredInstances: 5 }),
    ];
    const html = scalingHistorySection(decisions);
    assert.ok(html.includes("Scaling History"));
    assert.ok(html.includes(">2<"));
    assert.ok(html.includes(">5<"));
  });

  await it("highlights clamped rows", () => {
    const decisions = [makeScaleDecision({ desiredInstances: 5, unclampedDesiredInstances: 12 })];
    const html = scalingHistorySection(decisions);
    assert.ok(html.includes("row-highlight"));
    assert.ok(html.includes("(clamped)"));
  });

  await it("does not highlight unclamped rows", () => {
    const decisions = [makeScaleDecision({ desiredInstances: 3, unclampedDesiredInstances: 3 })];
    const html = scalingHistorySection(decisions);
    assert.ok(!html.includes("row-highlight"));
    assert.ok(!html.includes("(clamped)"));
  });
});
