import { describe, expect, it } from "vitest";
import { makeClusterState, makeInstance, makeSupervisor, makeTimestamp } from "../test-helpers.ts";
import { instancePage, lifecycleTimeline } from "./instance.ts";

describe("instancePage", () => {
  it("shows not found for unknown instance name", async () => {
    const state = makeClusterState();
    const html = await instancePage(state, "unknown-pod").text();
    expect(html).toContain("Instance not found");
    expect(html).toContain("unknown-pod");
  });

  it("shows ready badge for ready instance", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain("badge-green");
    expect(html).toContain(">ready<");
  });

  it("shows pending badge for pending instance", async () => {
    const inst = makeInstance({ name: "pod-2", readyAt: undefined });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-2").text();
    expect(html).toContain("badge-yellow");
    expect(html).toContain(">pending<");
  });

  it("renders CPU and memory values", async () => {
    const inst = makeInstance({ name: "pod-1", cpuUsageMilli: 250, memoryUsageMib: 128 });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain(">250m<");
    expect(html).toContain(">128Mi<");
  });

  it("links to parent function", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain('href="/functions/');
    expect(html).toContain(">default:web-app:tenant-1</a>");
  });

  it("links to responsible controller", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst], responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain('href="/controllers/10.0.0.42"');
    expect(html).toContain(">10.0.0.42</a>");
  });

  it("includes HTMX polling", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("includes lifecycle timeline", async () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await instancePage(state, "pod-1").text();
    expect(html).toContain("timeline");
    expect(html).toContain("Assigned");
    expect(html).toContain("Ready");
    expect(html).toContain("Serving");
  });

  it("returns correct content-type", () => {
    const inst = makeInstance({ name: "pod-1" });
    const sup = makeSupervisor({ instances: [inst] });
    const state = makeClusterState({ supervisors: [sup] });
    const response = instancePage(state, "pod-1");
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});

describe("lifecycleTimeline", () => {
  it("shows active dots for ready instance", () => {
    const inst = makeInstance();
    const html = lifecycleTimeline(inst);
    // Count active dots (without inactive class)
    const dots = html.match(/step-dot"/g);
    expect(dots).toHaveLength(3);
  });

  it("shows inactive dots for pending instance", () => {
    const inst = makeInstance({ readyAt: undefined });
    const html = lifecycleTimeline(inst);
    expect(html).toContain("inactive");
  });

  it("shows slow startup warning for > 30s startup", () => {
    const assignedAt = makeTimestamp(-60_000); // 60s ago
    const readyAt = makeTimestamp(-20_000); // 20s ago (40s startup)
    const inst = makeInstance({ assignedAt, readyAt });
    const html = lifecycleTimeline(inst);
    expect(html).toContain("slow");
  });

  it("does not show slow warning for quick startup", () => {
    const assignedAt = makeTimestamp(-60_000);
    const readyAt = makeTimestamp(-55_000); // 5s startup
    const inst = makeInstance({ assignedAt, readyAt });
    const html = lifecycleTimeline(inst);
    expect(html).not.toContain("slow");
  });
});
