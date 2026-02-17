import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeInstance, makeSupervisor, makeTimestamp } from "../test-helpers.ts";
import { instancePage, lifecycleTimeline } from "./instance.ts";

await describe("instancePage", async () => {
  await it("shows not found for unknown instance name", async () => {
    const state = makeClusterState();
    const html = await instancePage(state, "unknown-pod").text();
    assert.ok(html.includes("Instance not found"));
    assert.ok(html.includes("unknown-pod"));
  });

  await it("shows ready badge for ready instance", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes("badge-green"));
    assert.ok(html.includes(">ready<"));
  });

  await it("shows pending badge for pending instance", async () => {
    const inst = makeInstance({ name: "pod-2", readyAt: undefined });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-2").text();
    assert.ok(html.includes("badge-yellow"));
    assert.ok(html.includes(">pending<"));
  });

  await it("renders CPU and memory values", async () => {
    const inst = makeInstance({ name: "pod-1", cpuUsageMilli: 250, memoryUsageMib: 128 });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes(">250m<"));
    assert.ok(html.includes(">128Mi<"));
  });

  await it("links to parent function", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes('href="/functions/'));
    assert.ok(html.includes(">default:web-app:tenant-1</a>"));
  });

  await it("links to responsible controller", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst], responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes('href="/controllers/10.0.0.42"'));
    assert.ok(html.includes(">10.0.0.42</a>"));
  });

  await it("includes HTMX polling", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("includes lifecycle timeline", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    assert.ok(html.includes("timeline"));
    assert.ok(html.includes("Assigned"));
    assert.ok(html.includes("Ready"));
    assert.ok(html.includes("Serving"));
  });

  await it("returns correct content-type", () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const response = instancePage(state, "pod-1");
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});

await describe("lifecycleTimeline", async () => {
  await it("shows active dots for ready instance", () => {
    const inst = makeInstance();
    const html = lifecycleTimeline(inst);
    // Count active dots (without inactive class)
    const dots = html.match(/step-dot"/g);
    assert.ok(dots && dots.length === 3);
  });

  await it("shows inactive dots for pending instance", () => {
    const inst = makeInstance({ readyAt: undefined });
    const html = lifecycleTimeline(inst);
    assert.ok(html.includes("inactive"));
  });

  await it("shows slow startup warning for > 30s startup", () => {
    const assignedAt = makeTimestamp(-60_000); // 60s ago
    const readyAt = makeTimestamp(-20_000); // 20s ago (40s startup)
    const inst = makeInstance({ assignedAt, readyAt });
    const html = lifecycleTimeline(inst);
    assert.ok(html.includes("slow"));
  });

  await it("does not show slow warning for quick startup", () => {
    const assignedAt = makeTimestamp(-60_000);
    const readyAt = makeTimestamp(-55_000); // 5s startup
    const inst = makeInstance({ assignedAt, readyAt });
    const html = lifecycleTimeline(inst);
    assert.ok(!html.includes("slow"));
  });
});
