import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makeClusterState,
  makeSupervisor,
  makeFunction,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { routerPage } from "./router.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

await describe("routerPage", async () => {
  await it("shows not found for unknown router IP", async () => {
    const state = makeClusterState();
    const html = await routerPage(state, "10.0.0.99").text();
    assert.ok(html.includes("Router not found"));
    assert.ok(html.includes("10.0.0.99"));
  });

  await it("counts functions this router has heartbeats for", async () => {
    const hb1 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const hb2 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup1 = makeSupervisor({ routerHeartbeats: [hb1] });
    const sup2 = makeSupervisor({
      function: makeFunction({ deployment: "api" }),
      routerHeartbeats: [hb2],
    });
    const state = makeClusterState({ supervisors: [sup1, sup2] });
    const html = await routerPage(state, "10.0.1.1").text();
    assert.ok(html.includes(">Functions<"));
    assert.ok(html.includes(">2<"));
  });

  await it("aggregates total in-flight across functions", async () => {
    const hb1 = makeHeartbeatState({
      routerIp: "10.0.1.1",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 3 }),
    });
    const hb2 = makeHeartbeatState({
      routerIp: "10.0.1.1",
      heartbeat: create(HeartbeatSchema, { inFlightRequests: 7 }),
    });
    const sup1 = makeSupervisor({ routerHeartbeats: [hb1] });
    const sup2 = makeSupervisor({
      function: makeFunction({ deployment: "api" }),
      routerHeartbeats: [hb2],
    });
    const state = makeClusterState({ supervisors: [sup1, sup2] });
    const html = await routerPage(state, "10.0.1.1").text();
    assert.ok(html.includes(">Total In-Flight<"));
    assert.ok(html.includes(">10<"));
  });

  await it("renders function rows with links", async () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await routerPage(state, "10.0.1.1").text();
    assert.ok(html.includes('href="/functions/'));
    assert.ok(html.includes(">web-app</a>"));
  });

  await it("includes HTMX polling", async () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await routerPage(state, "10.0.1.1").text();
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("returns correct content-type", () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const response = routerPage(state, "10.0.1.1");
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
