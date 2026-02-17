import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeSupervisor, makeFunction, makeTimestamp } from "../test-helpers.ts";
import { controllerPage } from "./controller.ts";

await describe("controllerPage", async () => {
  await it("shows not found for unknown IP", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const html = await controllerPage(state, "10.0.0.99").text();
    assert.ok(html.includes("Controller not found"));
    assert.ok(html.includes("10.0.0.99"));
  });

  await it("shows self badge when IP matches podIp", async () => {
    const state = makeClusterState({ podIp: "10.0.0.100", controllerIps: ["10.0.0.100"] });
    const html = await controllerPage(state, "10.0.0.100").text();
    assert.ok(html.includes("badge-green"));
    assert.ok(html.includes(">self<"));
  });

  await it("does not show self badge for other controllers", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100", "10.0.0.101"],
    });
    const html = await controllerPage(state, "10.0.0.101").text();
    assert.ok(!html.includes(">self<"));
  });

  await it("counts functions assigned to this controller", async () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({
      function: makeFunction({ deployment: "api" }),
      responsibleControllerIp: "10.0.0.1",
    });
    const sup3 = makeSupervisor({
      function: makeFunction({ deployment: "other" }),
      responsibleControllerIp: "10.0.0.2",
    });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2, sup3],
    });
    const html = await controllerPage(state, "10.0.0.1").text();
    // Functions card should show 2
    assert.ok(html.includes(">Functions<"));
    assert.ok(html.includes(">2<"));
  });

  await it("shows uptime for self controller", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100"],
      startedAt: makeTimestamp(-3_600_000),
    });
    const html = await controllerPage(state, "10.0.0.100").text();
    assert.ok(html.includes(">Uptime<"));
    assert.ok(!html.includes(">Uptime<") || !html.match(/>Uptime<[\s\S]*?>—</));
  });

  await it("shows dash for uptime on non-self controller", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100", "10.0.0.101"],
    });
    const html = await controllerPage(state, "10.0.0.101").text();
    assert.ok(html.includes(">Uptime<"));
    // The value after Uptime should be a dash
    assert.ok(html.includes(">—<"));
  });

  await it("renders function table with links", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [sup],
    });
    const html = await controllerPage(state, "10.0.0.1").text();
    assert.ok(html.includes('href="/functions/'));
    assert.ok(html.includes(">web-app</a>"));
  });

  await it("shows empty message when no functions assigned", async () => {
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [],
    });
    const html = await controllerPage(state, "10.0.0.1").text();
    assert.ok(html.includes("No functions assigned"));
  });

  await it("includes HTMX auto-refresh attributes", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const html = await controllerPage(state, "10.0.0.1").text();
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("returns correct content-type", () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const response = controllerPage(state, "10.0.0.1");
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
