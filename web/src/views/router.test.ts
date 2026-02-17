import { describe, expect, it } from "vitest";
import {
  makeClusterState,
  makeSupervisor,
  makeFunction,
  makeHeartbeatState,
} from "../test-helpers.ts";
import { routerPage } from "./router.ts";
import { create } from "@bufbuild/protobuf";
import { HeartbeatSchema } from "../gen/types_pb.ts";

describe("routerPage", () => {
  it("shows not found for unknown router IP", async () => {
    const state = makeClusterState();
    const html = await routerPage(state, "10.0.0.99").text();
    expect(html).toContain("Router not found");
    expect(html).toContain("10.0.0.99");
  });

  it("counts functions this router has heartbeats for", async () => {
    const hb1 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const hb2 = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup1 = makeSupervisor({ routerHeartbeats: [hb1] });
    const sup2 = makeSupervisor({
      function: makeFunction({ deployment: "api" }),
      routerHeartbeats: [hb2],
    });
    const state = makeClusterState({ supervisors: [sup1, sup2] });
    const html = await routerPage(state, "10.0.1.1").text();
    expect(html).toContain(">Functions<");
    expect(html).toContain(">2<");
  });

  it("aggregates total in-flight across functions", async () => {
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
    expect(html).toContain(">Total In-Flight<");
    expect(html).toContain(">10<");
  });

  it("renders function rows with links", async () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await routerPage(state, "10.0.1.1").text();
    expect(html).toContain('href="/functions/');
    expect(html).toContain(">web-app</a>");
  });

  it("includes HTMX polling", async () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const html = await routerPage(state, "10.0.1.1").text();
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("returns correct content-type", () => {
    const hb = makeHeartbeatState({ routerIp: "10.0.1.1" });
    const sup = makeSupervisor({ routerHeartbeats: [hb] });
    const state = makeClusterState({ supervisors: [sup] });
    const response = routerPage(state, "10.0.1.1");
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
