import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeInstance } from "../../test-helpers.ts";
import { instancesTable, instancesPartial } from "./instances.ts";

await describe("instancesTable", async () => {
  await it("shows empty message for no instances", () => {
    const html = instancesTable([]);
    assert.ok(html.includes("No instances"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders a ready instance with green badge and name link", () => {
    const inst = makeInstance();
    const html = instancesTable([inst]);
    assert.ok(html.includes("badge-green"));
    assert.ok(html.includes(">ready<"));
    assert.ok(html.includes('href="/instances/pod-abc123"'));
    assert.ok(html.includes(">pod-abc123</a>"));
    assert.ok(html.includes("10.0.0.1:8080"));
    assert.ok(html.includes("100m"));
    assert.ok(html.includes("64Mi"));
    assert.ok(html.includes("web-app-rs1"));
  });

  await it("renders a pending instance with yellow badge", () => {
    const inst = makeInstance({ readyAt: undefined });
    const html = instancesTable([inst]);
    assert.ok(html.includes("badge-yellow"));
    assert.ok(html.includes(">pending<"));
    // Ready column should show dash
    assert.ok(html.includes("—"));
  });

  await it("renders multiple instances as multiple rows", () => {
    const instances = [makeInstance({ name: "pod-1" }), makeInstance({ name: "pod-2" })];
    const html = instancesTable(instances);
    assert.ok(html.includes("pod-1"));
    assert.ok(html.includes("pod-2"));
    // Should have table headers
    assert.ok(html.includes("<th>Name</th>"));
    assert.ok(html.includes("<th>Address</th>"));
    assert.ok(html.includes("<th>Status</th>"));
    assert.ok(html.includes("<th>CPU</th>"));
    assert.ok(html.includes("<th>Memory</th>"));
    assert.ok(html.includes("<th>ReplicaSet</th>"));
    assert.ok(html.includes("<th>Assigned</th>"));
    assert.ok(html.includes("<th>Ready</th>"));
  });

  await it("shows stale badge when instance replica set differs from active", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "web-app-rs2");
    assert.ok(html.includes(">stale<"));
    assert.ok(html.includes("badge-yellow"));
  });

  await it("does not show stale badge when replica set matches active", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "web-app-rs1");
    assert.ok(!html.includes(">stale<"));
  });

  await it("does not show stale badge when no active replica set", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst]);
    assert.ok(!html.includes(">stale<"));
  });

  await it("does not show stale badge when active replica set is empty string", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "");
    assert.ok(!html.includes(">stale<"));
  });
});

await describe("instancesPartial", async () => {
  await it("returns a Response with text/html content-type", async () => {
    const response = instancesPartial([]);
    assert.ok(response instanceof Response);
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
    const body = await response.text();
    assert.ok(body.includes("No instances"));
  });
});
