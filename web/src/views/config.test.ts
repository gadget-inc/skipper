import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeConfigValue } from "../test-helpers.ts";
import { configPage } from "./config.ts";

await describe("configPage", async () => {
  await it("shows empty state when no config values", async () => {
    const state = makeClusterState({ config: [] });
    const html = await configPage(state).text();
    assert.ok(html.includes("No configuration values"));
  });

  await it("renders config table with values", async () => {
    const cv1 = makeConfigValue({
      name: "heartbeat-timeout",
      value: "90s",
      description: "Heartbeat timeout",
    });
    const cv2 = makeConfigValue({
      name: "scale-interval",
      value: "15s",
      description: "Scale interval",
    });
    const state = makeClusterState({ config: [cv1, cv2] });
    const html = await configPage(state).text();
    assert.ok(html.includes("heartbeat-timeout"));
    assert.ok(html.includes("90s"));
    assert.ok(html.includes("Heartbeat timeout"));
    assert.ok(html.includes("scale-interval"));
    assert.ok(html.includes("15s"));
  });

  await it("masks sensitive values", async () => {
    const cv = makeConfigValue({
      name: "paseto-private-key",
      value: "****",
      description: "Private key",
    });
    const state = makeClusterState({ config: [cv] });
    const html = await configPage(state).text();
    assert.ok(html.includes("****"));
    assert.ok(!html.includes("actual-key"));
  });

  await it("returns correct content-type", () => {
    const response = configPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
