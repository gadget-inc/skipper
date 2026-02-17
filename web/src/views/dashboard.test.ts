import { describe, expect, it } from "vitest";
import {
  makeClusterState,
  makeEvent,
  makeInstance,
  makeSupervisor,
  makeTimestamp,
} from "../test-helpers.ts";
import { dashboardPage } from "./dashboard.ts";

describe("dashboardPage", () => {
  it("shows zero counts for empty state", async () => {
    const state = makeClusterState({ supervisors: [], controllerIps: [] });
    const response = dashboardPage(state);
    const html = await response.text();

    expect(html).toContain("Dashboard");
    // Controllers: 0
    expect(html).toContain(">Controllers<");
    expect(html).toContain(">0<");
    // Functions: 0
    expect(html).toContain(">Functions<");
    // Instances: 0/0
    expect(html).toContain(">0/0<");
  });

  it("shows correct counts for populated state", async () => {
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
    expect(html).toContain(">3<");
    // 2 functions
    expect(html).toContain(">2<");
    // 2 ready / 3 total instances
    expect(html).toContain(">2/3<");
  });

  it("renders controller ring with IPs as links joined by separator", async () => {
    const state = makeClusterState({ controllerIps: ["10.0.0.1", "10.0.0.2"] });
    const html = await dashboardPage(state).text();
    expect(html).toContain('href="/controllers/10.0.0.1"');
    expect(html).toContain('href="/controllers/10.0.0.2"');
    expect(html).toContain(" · ");
  });

  it("shows self IP in controller ring", async () => {
    const state = makeClusterState({ podIp: "10.0.0.100" });
    const html = await dashboardPage(state).text();
    expect(html).toContain("(self: 10.0.0.100)");
  });

  it("shows uptime from startedAt", async () => {
    const state = makeClusterState({ startedAt: makeTimestamp(-3_600_000) });
    const html = await dashboardPage(state).text();
    // Should show "Uptime" label and some time string (1h ago)
    expect(html).toContain(">Uptime<");
  });

  it("embeds supervisors table with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    expect(html).toContain('hx-get="/partials/supervisors"');
    expect(html).toContain('hx-trigger="every 5s"');
    expect(html).toContain('hx-swap="innerHTML"');
  });

  it("includes health indicators with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    expect(html).toContain('hx-get="/partials/health"');
    expect(html).toContain("health-indicators");
  });

  it("includes recent activity section with HTMX polling", async () => {
    const html = await dashboardPage(makeClusterState()).text();
    expect(html).toContain("Recent Activity");
    expect(html).toContain('hx-get="/partials/recent-activity"');
  });

  it("renders recent events when present", async () => {
    const state = makeClusterState({
      events: [makeEvent({ message: "test scale event" })],
    });
    const html = await dashboardPage(state).text();
    expect(html).toContain("test scale event");
  });

  it("returns full HTML document with correct content-type", () => {
    const response = dashboardPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
