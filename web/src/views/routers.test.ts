import { describe, expect, it } from "vitest";
import {
  makeClusterState,
  makeSupervisor,
  makeFunction,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { routersPage } from "./routers.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

describe("routersPage", () => {
  it("shows empty message when no routers", async () => {
    const state = makeClusterState({ supervisors: [] });
    const html = await routersPage(state).text();
    expect(html).toContain("No routers");
    expect(html).toContain('class="empty"');
  });

  it("renders unique router IP links", async () => {
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
    expect(html).toContain('href="/routers/10.0.1.1"');
    expect(html).toContain('href="/routers/10.0.1.2"');
    expect(html).toContain(">10.0.1.1</a>");
    expect(html).toContain(">10.0.1.2</a>");
  });

  it("aggregates function count and in-flight per router", async () => {
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
    expect(html).toContain(">2<");
    expect(html).toContain(">10<");
  });

  it("includes HTMX auto-refresh attributes", async () => {
    const html = await routersPage(makeClusterState()).text();
    expect(html).toContain('hx-get="/routers"');
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("returns correct content-type", () => {
    const response = routersPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
