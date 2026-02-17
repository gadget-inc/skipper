import { describe, expect, it } from "vitest";
import { makeClusterState, makeSupervisor } from "../test-helpers.ts";
import { controllersPage, ringDistribution } from "./controllers.ts";

describe("controllersPage", () => {
  it("shows empty message when no controllers", async () => {
    const state = makeClusterState({ controllerIps: [], supervisors: [] });
    const html = await controllersPage(state).text();
    expect(html).toContain("No controllers");
    expect(html).toContain('class="empty"');
  });

  it("renders IP links to detail pages", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await controllersPage(state).text();
    expect(html).toContain('href="/controllers/10.0.0.1"');
    expect(html).toContain('href="/controllers/10.0.0.2"');
    expect(html).toContain(">10.0.0.1</a>");
    expect(html).toContain(">10.0.0.2</a>");
  });

  it("shows self badge for matching podIp", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.1",
      controllerIps: ["10.0.0.1", "10.0.0.2"],
    });
    const html = await controllersPage(state).text();
    expect(html).toContain("badge-green");
    expect(html).toContain(">self<");
  });

  it("shows function and instance counts per controller", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [sup],
    });
    const html = await controllersPage(state).text();
    // 1 function, 1/1 instances
    expect(html).toContain(">1<");
  });

  it("includes HTMX auto-refresh attributes", async () => {
    const html = await controllersPage(makeClusterState()).text();
    expect(html).toContain('hx-get="/controllers"');
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("includes ring distribution section", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await controllersPage(state).text();
    expect(html).toContain("Ring Distribution");
    expect(html).toContain("dist-chart");
  });

  it("returns correct content-type", () => {
    const response = controllersPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});

describe("ringDistribution", () => {
  it("returns empty string when no controllers", () => {
    const state = makeClusterState({ controllerIps: [], supervisors: [] });
    expect(ringDistribution(state)).toBe("");
  });

  it("renders bar for each controller IP", () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = ringDistribution(state);
    expect(html).toContain("dist-row");
    expect(html).toContain("10.0.0.1");
    expect(html).toContain("10.0.0.2");
  });

  it("shows function counts per controller", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup3 = makeSupervisor({ responsibleControllerIp: "10.0.0.2" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2, sup3],
    });
    const html = ringDistribution(state);
    // 10.0.0.1 has 2, 10.0.0.2 has 1
    expect(html).toContain('class="dist-count">2<');
    expect(html).toContain('class="dist-count">1<');
  });

  it("shows imbalanced badge when distribution is skewed", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup3 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup4 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2, sup3, sup4],
    });
    const html = ringDistribution(state);
    expect(html).toContain("imbalanced");
    expect(html).toContain("badge-yellow");
  });

  it("does not show imbalanced badge for even distribution", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.2" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2],
    });
    const html = ringDistribution(state);
    expect(html).not.toContain("imbalanced");
  });
});
