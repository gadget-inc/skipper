import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeSupervisor } from "../../test-helpers.ts";
import { supervisorsTable, supervisorsPartial } from "./supervisors.ts";

await describe("supervisorsTable", async () => {
  await it("shows empty message when no supervisors", () => {
    const state = makeClusterState({ supervisors: [] });
    const html = supervisorsTable(state);
    assert.ok(html.includes("No active supervisors"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders deployment link with URL-encoded key", () => {
    const state = makeClusterState();
    const html = supervisorsTable(state);
    // namespace:deployment:tenant → colons encoded as %3A
    assert.ok(html.includes("%3A"));
    assert.ok(html.includes('href="/functions/'));
    assert.ok(html.includes(">web-app</a>"));
  });

  await it("shows green badge for responsible controller with link", () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.100" });
    const state = makeClusterState({ podIp: "10.0.0.100", supervisors: [sup] });
    const html = supervisorsTable(state);
    assert.ok(html.includes("badge-green"));
    assert.ok(html.includes(">yes<"));
    assert.ok(html.includes('href="/controllers/10.0.0.100"'));
  });

  await it("shows muted badge for non-responsible controller with link", () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.200" });
    const state = makeClusterState({ podIp: "10.0.0.100", supervisors: [sup] });
    const html = supervisorsTable(state);
    assert.ok(html.includes("badge-muted"));
    assert.ok(html.includes(">no<"));
    assert.ok(html.includes('href="/controllers/10.0.0.200"'));
  });

  await it("renders all table headers", () => {
    const state = makeClusterState();
    const html = supervisorsTable(state);
    assert.ok(html.includes("<th>Deployment</th>"));
    assert.ok(html.includes("<th>Namespace</th>"));
    assert.ok(html.includes("<th>Tenant</th>"));
    assert.ok(html.includes("<th>Instances</th>"));
    assert.ok(html.includes("<th>Heartbeats</th>"));
    assert.ok(html.includes("<th>Responsible</th>"));
  });
});

await describe("supervisorsPartial", async () => {
  await it("returns a Response with text/html content-type", async () => {
    const state = makeClusterState({ supervisors: [] });
    const response = supervisorsPartial(state);
    assert.ok(response instanceof Response);
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
    const body = await response.text();
    assert.ok(body.includes("No active supervisors"));
  });
});
