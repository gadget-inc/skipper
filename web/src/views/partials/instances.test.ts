import { describe, expect, it } from "vitest";
import { makeInstance } from "../../test-helpers.ts";
import { instancesTable, instancesPartial } from "./instances.ts";

describe("instancesTable", () => {
  it("shows empty message for no instances", () => {
    const html = instancesTable([]);
    expect(html).toContain("No instances");
    expect(html).toContain('class="empty"');
  });

  it("renders a ready instance with green badge and name link", () => {
    const inst = makeInstance();
    const html = instancesTable([inst]);
    expect(html).toContain("badge-green");
    expect(html).toContain(">ready<");
    expect(html).toContain('href="/instances/pod-abc123"');
    expect(html).toContain(">pod-abc123</a>");
    expect(html).toContain("10.0.0.1:8080");
    expect(html).toContain("100m");
    expect(html).toContain("64Mi");
    expect(html).toContain("web-app-rs1");
  });

  it("renders a pending instance with yellow badge", () => {
    const inst = makeInstance({ readyAt: undefined });
    const html = instancesTable([inst]);
    expect(html).toContain("badge-yellow");
    expect(html).toContain(">pending<");
    // Ready column should show dash
    expect(html).toContain("—");
  });

  it("renders multiple instances as multiple rows", () => {
    const instances = [makeInstance({ name: "pod-1" }), makeInstance({ name: "pod-2" })];
    const html = instancesTable(instances);
    expect(html).toContain("pod-1");
    expect(html).toContain("pod-2");
    // Should have table headers
    expect(html).toContain("<th>Name</th>");
    expect(html).toContain("<th>Address</th>");
    expect(html).toContain("<th>Status</th>");
    expect(html).toContain("<th>CPU</th>");
    expect(html).toContain("<th>Memory</th>");
    expect(html).toContain("<th>ReplicaSet</th>");
    expect(html).toContain("<th>Assigned</th>");
    expect(html).toContain("<th>Ready</th>");
  });

  it("shows stale badge when instance replica set differs from active", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "web-app-rs2");
    expect(html).toContain(">stale<");
    expect(html).toContain("badge-yellow");
  });

  it("does not show stale badge when replica set matches active", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "web-app-rs1");
    expect(html).not.toContain(">stale<");
  });

  it("does not show stale badge when no active replica set", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst]);
    expect(html).not.toContain(">stale<");
  });

  it("does not show stale badge when active replica set is empty string", () => {
    const inst = makeInstance({ replicaSet: "web-app-rs1" });
    const html = instancesTable([inst], "");
    expect(html).not.toContain(">stale<");
  });
});

describe("instancesPartial", () => {
  it("returns a Response with text/html content-type", async () => {
    const response = instancesPartial([]);
    expect(response).toBeInstanceOf(Response);
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
    const body = await response.text();
    expect(body).toContain("No instances");
  });
});
