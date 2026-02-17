import { describe, expect, it } from "vitest";
import { makeClusterState, makeSupervisor, makeFunction, makeTimestamp } from "../test-helpers.ts";
import { controllerPage } from "./controller.ts";

describe("controllerPage", () => {
  it("shows not found for unknown IP", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const html = await controllerPage(state, "10.0.0.99").text();
    expect(html).toContain("Controller not found");
    expect(html).toContain("10.0.0.99");
  });

  it("shows self badge when IP matches podIp", async () => {
    const state = makeClusterState({ podIp: "10.0.0.100", controllerIps: ["10.0.0.100"] });
    const html = await controllerPage(state, "10.0.0.100").text();
    expect(html).toContain("badge-green");
    expect(html).toContain(">self<");
  });

  it("does not show self badge for other controllers", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100", "10.0.0.101"],
    });
    const html = await controllerPage(state, "10.0.0.101").text();
    expect(html).not.toContain(">self<");
  });

  it("counts functions assigned to this controller", async () => {
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
    expect(html).toContain(">Functions<");
    expect(html).toContain(">2<");
  });

  it("shows uptime for self controller", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100"],
      startedAt: makeTimestamp(-3_600_000),
    });
    const html = await controllerPage(state, "10.0.0.100").text();
    expect(html).toContain(">Uptime<");
    expect(html).not.toMatch(/>Uptime<[\s\S]*?>—</);
  });

  it("shows dash for uptime on non-self controller", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.100",
      controllerIps: ["10.0.0.100", "10.0.0.101"],
    });
    const html = await controllerPage(state, "10.0.0.101").text();
    expect(html).toContain(">Uptime<");
    // The value after Uptime should be a dash
    expect(html).toContain(">—<");
  });

  it("renders function table with links", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [sup],
    });
    const html = await controllerPage(state, "10.0.0.1").text();
    expect(html).toContain('href="/functions/');
    expect(html).toContain(">web-app</a>");
  });

  it("shows empty message when no functions assigned", async () => {
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [],
    });
    const html = await controllerPage(state, "10.0.0.1").text();
    expect(html).toContain("No functions assigned");
  });

  it("includes HTMX auto-refresh attributes", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const html = await controllerPage(state, "10.0.0.1").text();
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("returns correct content-type", () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1"] });
    const response = controllerPage(state, "10.0.0.1");
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
