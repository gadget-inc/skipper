import { describe, expect, it } from "vitest";
import { makeClusterState, makeConfigValue } from "../test-helpers.ts";
import { configPage } from "./config.ts";

describe("configPage", () => {
  it("shows empty state when no config values", async () => {
    const state = makeClusterState({ config: [] });
    const html = await configPage(state).text();
    expect(html).toContain("No configuration values");
  });

  it("renders config table with values", async () => {
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
    expect(html).toContain("heartbeat-timeout");
    expect(html).toContain("90s");
    expect(html).toContain("Heartbeat timeout");
    expect(html).toContain("scale-interval");
    expect(html).toContain("15s");
  });

  it("masks sensitive values", async () => {
    const cv = makeConfigValue({
      name: "paseto-private-key",
      value: "****",
      description: "Private key",
    });
    const state = makeClusterState({ config: [cv] });
    const html = await configPage(state).text();
    expect(html).toContain("****");
    expect(html).not.toContain("actual-key");
  });

  it("returns correct content-type", () => {
    const response = configPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
