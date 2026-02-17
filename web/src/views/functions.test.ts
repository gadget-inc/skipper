import { describe, expect, it } from "vitest";
import {
  makeClusterState,
  makeSupervisor,
  makeFunction,
  makeScale,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { functionsPage } from "./functions.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

describe("functionsPage", () => {
  it("shows empty message when no functions", async () => {
    const state = makeClusterState({ supervisors: [] });
    const html = await functionsPage(state).text();
    expect(html).toContain("No functions");
    expect(html).toContain('class="empty"');
  });

  it("renders all 8 column headers", async () => {
    const state = makeClusterState();
    const html = await functionsPage(state).text();
    expect(html).toContain("<th>Deployment</th>");
    expect(html).toContain("<th>Namespace</th>");
    expect(html).toContain("<th>Tenant</th>");
    expect(html).toContain("<th>Instances</th>");
    expect(html).toContain("<th>Scale</th>");
    expect(html).toContain("<th>Routers</th>");
    expect(html).toContain("<th>In-Flight</th>");
    expect(html).toContain("<th>Responsible</th>");
  });

  it("renders scale range as min–max", async () => {
    const fn = makeFunction({ scale: makeScale({ minInstances: 2, maxInstances: 10 }) });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    expect(html).toContain("2–10");
  });

  it("renders dash when no scale configured", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    // The dash should be in the scale column
    expect(html).toContain(">—<");
  });

  it("computes router count and total in-flight correctly", async () => {
    const hb1 = makeHeartbeatState({
      routerIp: "10.0.1.1",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 3 }),
    });
    const hb2 = makeHeartbeatState({
      routerIp: "10.0.1.2",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 7 }),
    });
    const sup = makeSupervisor({ routerHeartbeats: [hb1, hb2] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    // 2 routers
    expect(html).toContain(">2<");
    // 10 total in-flight
    expect(html).toContain(">10<");
  });

  it("includes HTMX auto-refresh attributes", async () => {
    const html = await functionsPage(makeClusterState()).text();
    expect(html).toContain('hx-get="/functions"');
    expect(html).toContain('hx-trigger="every 5s"');
    expect(html).toContain('hx-swap="innerHTML"');
    expect(html).toContain('hx-select="main > *"');
    expect(html).toContain('hx-target="main"');
  });

  it("renders responsible controller IP as link", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    expect(html).toContain('href="/controllers/10.0.0.42"');
    expect(html).toContain(">10.0.0.42</a>");
  });

  it("returns full HTML document with correct content-type", () => {
    const response = functionsPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
