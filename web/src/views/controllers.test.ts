import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeSupervisor } from "../test-helpers.ts";
import { controllersPage, ringDistribution } from "./controllers.ts";

await describe("controllersPage", async () => {
  await it("shows empty message when no controllers", async () => {
    const state = makeClusterState({ controllerIps: [], supervisors: [] });
    const html = await controllersPage(state).text();
    assert.ok(html.includes("No controllers"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders IP links to detail pages", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await controllersPage(state).text();
    assert.ok(html.includes('href="/controllers/10.0.0.1"'));
    assert.ok(html.includes('href="/controllers/10.0.0.2"'));
    assert.ok(html.includes(">10.0.0.1</a>"));
    assert.ok(html.includes(">10.0.0.2</a>"));
  });

  await it("shows self badge for matching podIp", async () => {
    const state = makeClusterState({
      podIp: "10.0.0.1",
      controllerIps: ["10.0.0.1", "10.0.0.2"],
    });
    const html = await controllersPage(state).text();
    assert.ok(html.includes("badge-green"));
    assert.ok(html.includes(">self<"));
  });

  await it("shows function and instance counts per controller", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1"],
      supervisors: [sup],
    });
    const html = await controllersPage(state).text();
    // 1 function, 1/1 instances
    assert.ok(html.includes(">1<"));
  });

  await it("includes HTMX auto-refresh attributes", async () => {
    const html = await controllersPage(makeClusterState()).text();
    assert.ok(html.includes('hx-get="/controllers"'));
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("includes ring distribution section", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await controllersPage(state).text();
    assert.ok(html.includes("Ring Distribution"));
    assert.ok(html.includes("dist-chart"));
  });

  await it("returns correct content-type", () => {
    const response = controllersPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});

await describe("ringDistribution", async () => {
  await it("returns empty string when no controllers", () => {
    const state = makeClusterState({ controllerIps: [], supervisors: [] });
    assert.equal(ringDistribution(state), "");
  });

  await it("renders bar for each controller IP", () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = ringDistribution(state);
    assert.ok(html.includes("dist-row"));
    assert.ok(html.includes("10.0.0.1"));
    assert.ok(html.includes("10.0.0.2"));
  });

  await it("shows function counts per controller", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup3 = makeSupervisor({ responsibleControllerIp: "10.0.0.2" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2, sup3],
    });
    const html = ringDistribution(state);
    // 10.0.0.1 has 2, 10.0.0.2 has 1
    assert.ok(html.includes('class="dist-count">2<'));
    assert.ok(html.includes('class="dist-count">1<'));
  });

  await it("shows imbalanced badge when distribution is skewed", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup3 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup4 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2, sup3, sup4],
    });
    const html = ringDistribution(state);
    assert.ok(html.includes("imbalanced"));
    assert.ok(html.includes("badge-yellow"));
  });

  await it("does not show imbalanced badge for even distribution", () => {
    const sup1 = makeSupervisor({ responsibleControllerIp: "10.0.0.1" });
    const sup2 = makeSupervisor({ responsibleControllerIp: "10.0.0.2" });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2"],
      supervisors: [sup1, sup2],
    });
    const html = ringDistribution(state);
    assert.ok(!html.includes("imbalanced"));
  });
});
