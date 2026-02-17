import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makeClusterState,
  makeEvent,
  makeInstance,
  makeSupervisor,
  makeTimestamp,
} from "../test-helpers.ts";
import { dashboardPage } from "./dashboard.ts";

await describe("dashboardPage", async () => {
  await it("shows zero counts for empty state", async () => {
    const state = makeClusterState({ supervisors: [], controllerIps: [] });
    const response = dashboardPage(state);
    const html = await response.text();

    assert.ok(html.includes("Dashboard"));
    // Controllers: 0
    assert.ok(html.includes(">Controllers<"));
    assert.ok(html.includes(">0<"));
    // Functions: 0
    assert.ok(html.includes(">Functions<"));
    // Instances: 0/0
    assert.ok(html.includes(">0/0<"));
  });

  await it("shows correct counts for populated state", async () => {
    const sup1 = makeSupervisor({
      instances: [makeInstance(), makeInstance({ readyAt: undefined })],
    });
    const sup2 = makeSupervisor({
      function: { namespace: "prod", deployment: "api", tenant: "t2", metadata: "" },
      instances: [makeInstance()],
    });
    const state = makeClusterState({
      controllerIps: ["10.0.0.1", "10.0.0.2", "10.0.0.3"],
      supervisors: [sup1, sup2],
    });
    const html = await dashboardPage(state).text();

    // 3 controllers
    assert.ok(html.includes(">3<"));
    // 2 functions
    assert.ok(html.includes(">2<"));
    // 2 ready / 3 total instances
    assert.ok(html.includes(">2/3<"));
  });

  await it("renders controller ring with IPs as links joined by separator", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await dashboardPage(state).text();
    assert.ok(html.includes('href="/controllers/10.0.0.1"'));
    assert.ok(html.includes('href="/controllers/10.0.0.2"'));
    assert.ok(html.includes(" · "));
  });

  await it("shows self IP in controller ring", async () => {
    const state = makeClusterState({ podIp: "10.0.0.100" });
    const html = await dashboardPage(state).text();
    assert.ok(html.includes("(self: 10.0.0.100)"));
  });

  await it("shows uptime from startedAt", async () => {
    const state = makeClusterState({ startedAt: makeTimestamp(-3_600_000) });
    const html = await dashboardPage(state).text();
    // Should show "Uptime" label and some time string (1h ago)
    assert.ok(html.includes(">Uptime<"));
  });

  await it("embeds supervisors table with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    assert.ok(html.includes('hx-get="/partials/supervisors"'));
    assert.ok(html.includes('hx-trigger="every 5s"'));
    assert.ok(html.includes('hx-swap="innerHTML"'));
  });

  await it("includes health indicators with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    assert.ok(html.includes('hx-get="/partials/health"'));
    assert.ok(html.includes("health-indicators"));
  });

  await it("includes recent activity section with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    assert.ok(html.includes("Recent Activity"));
    assert.ok(html.includes('hx-get="/partials/recent-activity"'));
  });

  await it("renders recent events when present", async () => {
    const state = makeClusterState({
      events: [makeEvent({ message: "test scale event" })],
    });
    const html = await dashboardPage(state).text();
    assert.ok(html.includes("test scale event"));
  });

  await it("returns full HTML document with correct content-type", () => {
    const response = dashboardPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
