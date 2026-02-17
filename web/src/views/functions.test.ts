import { describe, it } from "node:test";
import assert from "node:assert/strict";
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

await describe("functionsPage", async () => {
  await it("shows empty message when no functions", async () => {
    const state = makeClusterState({ supervisors: [] });
    const html = await functionsPage(state).text();
    assert.ok(html.includes("No functions"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders all 8 column headers", async () => {
    const state = makeClusterState();
    const html = await functionsPage(state).text();
    assert.ok(html.includes("<th>Deployment</th>"));
    assert.ok(html.includes("<th>Namespace</th>"));
    assert.ok(html.includes("<th>Tenant</th>"));
    assert.ok(html.includes("<th>Instances</th>"));
    assert.ok(html.includes("<th>Scale</th>"));
    assert.ok(html.includes("<th>Routers</th>"));
    assert.ok(html.includes("<th>In-Flight</th>"));
    assert.ok(html.includes("<th>Responsible</th>"));
  });

  await it("renders scale range as min–max", async () => {
    const fn = makeFunction({ scale: makeScale({ minInstances: 2, maxInstances: 10 }) });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    assert.ok(html.includes("2–10"));
  });

  await it("renders dash when no scale configured", async () => {
    const fn = makeFunction({ scale: undefined });
    const sup = makeSupervisor({ function: fn });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    // The dash should be in the scale column
    assert.ok(html.includes(">—<"));
  });

  await it("computes router count and total in-flight correctly", async () => {
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
    assert.ok(html.includes(">2<"));
    // 10 total in-flight
    assert.ok(html.includes(">10<"));
  });

  await it("includes HTMX auto-refresh attributes", async () => {
    const html = await functionsPage(makeClusterState()).text();
    assert.ok(html.includes('hx-get="/functions"'));
    assert.ok(html.includes('hx-trigger="every 5s"'));
    assert.ok(html.includes('hx-swap="innerHTML"'));
    assert.ok(html.includes('hx-select="main > *"'));
    assert.ok(html.includes('hx-target="main"'));
  });

  await it("renders responsible controller IP as link", async () => {
    const sup = makeSupervisor({ responsibleControllerIp: "10.0.0.42" });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await functionsPage(state).text();
    assert.ok(html.includes('href="/controllers/10.0.0.42"'));
    assert.ok(html.includes(">10.0.0.42</a>"));
  });

  await it("returns full HTML document with correct content-type", () => {
    const response = functionsPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
