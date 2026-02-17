import { describe, expect, it } from "vitest";
import { makeClusterState, makeSupervisor } from "../../test-helpers.ts";
import { supervisorsTable, supervisorsPartial } from "./supervisors.ts";

describe("supervisorsTable", () => {
  it("shows empty message when no supervisors", () => {
    const state = makeClusterState({ supervisors: [] });
    const html = supervisorsTable(state);
    expect(html).toContain("No active supervisors");
    expect(html).toContain('class="empty"');
  });

  it("renders deployment link with URL-encoded key", () => {
    const state = makeClusterState();
    const html = supervisorsTable(state);
    // namespace:deployment:tenant → colons encoded as %3A
    expect(html).toContain("%3A");
    expect(html).toContain('href="/functions/');
    expect(html).toContain(">web-app</a>");
  });

  it("shows green badge for responsible controller with link", () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.100" });
    const state = makeClusterState({ podIp: "10.0.0.100", supervisors: [sup] });
    const html = supervisorsTable(state);
    expect(html).toContain("badge-green");
    expect(html).toContain(">yes<");
    expect(html).toContain('href="/controllers/10.0.0.100"');
  });

  it("shows muted badge for non-responsible controller with link", () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.200" });
    const state = makeClusterState({ podIp: "10.0.0.100", supervisors: [sup] });
    const html = supervisorsTable(state);
    expect(html).toContain("badge-muted");
    expect(html).toContain(">no<");
    expect(html).toContain('href="/controllers/10.0.0.200"');
  });

  it("renders all table headers", () => {
    const state = makeClusterState();
    const html = supervisorsTable(state);
    expect(html).toContain("<th>Deployment</th>");
    expect(html).toContain("<th>Namespace</th>");
    expect(html).toContain("<th>Tenant</th>");
    expect(html).toContain("<th>Instances</th>");
    expect(html).toContain("<th>Heartbeats</th>");
    expect(html).toContain("<th>Responsible</th>");
  });
});

describe("supervisorsPartial", () => {
  it("returns a Response with text/html content-type", async () => {
    const state = makeClusterState({ supervisors: [] });
    const response = supervisorsPartial(state);
    expect(response).toBeInstanceOf(Response);
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
    const body = await response.text();
    expect(body).toContain("No active supervisors");
  });
});
