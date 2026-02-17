import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  makeClusterState,
  makeSupervisor,
  makeFunction,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { routersPage } from "./routers.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

await describe("routersPage", async () => {
  await it("shows empty message when no routers", async () => {
    const state = makeClusterState({ supervisors: [] });
    const html = await routersPage(state).text();
    assert.ok(html.includes("No routers"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders unique router IP links", async () => {
    const hb1 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const hb2 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const hb3 = makeHeartbeatState({ routerIp: "10.0.1.2" });
    const sup1 = makeSupervisor({ routerHeartbeats: [hb1] });
    const sup2 = makeSupervisor({
      function: makeFunction({ deployment: "api" }),
      routerHeartbeats: [hb2, hb3],
    });
    const state = makeClusterState({ supervisors: [sup1, sup2] });
    const html = await routersPage(state).text();
    assert.ok(html.includes('href="/routers/10.0.1.1"'));
    assert.ok(html.includes('href="/routers/10.0.1.2"'));
    assert.ok(html.includes(">10.0.1.1</a>"));
    assert.ok(html.includes(">10.0.1.2</a>"));
  });

  await it("aggregates function count and in-flight per router", async () => {
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
    const html = await routersPage(state).text();
    // Router 10.0.1.1: 2 functions, 10 in-flight
    assert.ok(html.includes(">2<"));
    assert.ok(html.includes(">10<"));
  });

  await it("includes HTMX auto-refresh attributes", async () => {
    const html = await routersPage(makeClusterState()).text();
    assert.ok(html.includes('hx-get="/routers"'));
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("returns correct content-type", () => {
    const response = routersPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
