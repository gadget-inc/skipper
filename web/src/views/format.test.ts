import { describe, it, mock } from "node:test";
import assert from "node:assert/strict";
import { makeTimestamp } from "../test-helpers.ts";
import {
  timeAgo,
  formatTimestamp,
  scaleReasonLabel,
  functionKey,
  functionPath,
  durationBetween,
  eventTypeLabel,
  eventSeverityBadge,
} from "./format.ts";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";

await describe("timeAgo", async () => {
  await it("returns dash for undefined", () => {
    assert.equal(timeAgo(undefined), "—");
  });

  await it("returns 'just now' for future timestamps", () => {
    const ts = makeTimestamp(60_000);
    assert.equal(timeAgo(ts), "just now");
  });

  await it("returns seconds for <60s", () => {
    mock.timers.enable({ apis: ["Date"] });
    const now = Date.now();
    mock.timers.setTime(now);

    const ts = makeTimestamp(-30_000);
    assert.equal(timeAgo(ts), "30s ago");

    mock.timers.reset();
  });

  await it("returns minutes for <60m", () => {
    mock.timers.enable({ apis: ["Date"] });
    const now = Date.now();
    mock.timers.setTime(now);

    const ts = makeTimestamp(-5 * 60_000);
    assert.equal(timeAgo(ts), "5m ago");

    mock.timers.reset();
  });

  await it("returns hours for <24h", () => {
    mock.timers.enable({ apis: ["Date"] });
    const now = Date.now();
    mock.timers.setTime(now);

    const ts = makeTimestamp(-3 * 3_600_000);
    assert.equal(timeAgo(ts), "3h ago");

    mock.timers.reset();
  });

  await it("returns days for >=24h", () => {
    mock.timers.enable({ apis: ["Date"] });
    const now = Date.now();
    mock.timers.setTime(now);

    const ts = makeTimestamp(-2 * 86_400_000);
    assert.equal(timeAgo(ts), "2d ago");

    mock.timers.reset();
  });

  await it("returns 0s ago for exactly now", () => {
    mock.timers.enable({ apis: ["Date"] });
    const now = Date.now();
    mock.timers.setTime(now);

    const ts = makeTimestamp(0);
    assert.equal(timeAgo(ts), "0s ago");

    mock.timers.reset();
  });
});

await describe("formatTimestamp", async () => {
  await it("returns dash for undefined", () => {
    assert.equal(formatTimestamp(undefined), "—");
  });

  await it("returns ISO string for a known timestamp", () => {
    const ts = makeTimestamp(0);
    const result = formatTimestamp(ts);
    // Should be a valid ISO 8601 string
    assert.match(result, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });
});

await describe("durationBetween", async () => {
  await it("returns dash for undefined start", () => {
    assert.equal(durationBetween(undefined, makeTimestamp(0)), "—");
  });

  await it("returns dash for undefined end", () => {
    assert.equal(durationBetween(makeTimestamp(0), undefined), "—");
  });

  await it("returns seconds for short durations", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1005000);
    assert.equal(durationBetween(start, end), "5s");
  });

  await it("returns minutes and seconds for longer durations", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1090000);
    assert.equal(durationBetween(start, end), "1m 30s");
  });

  await it("returns minutes only when no remaining seconds", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1120000);
    assert.equal(durationBetween(start, end), "2m");
  });

  await it("returns 0s for zero duration", () => {
    const ts = timestampFromMs(1000000);
    assert.equal(durationBetween(ts, ts), "0s");
  });
});

await describe("scaleReasonLabel", async () => {
  await it("maps known reason codes to labels", () => {
    assert.equal(scaleReasonLabel(0), "unspecified");
    assert.equal(scaleReasonLabel(1), "cpu");
    assert.equal(scaleReasonLabel(2), "heartbeat_timeout");
    assert.equal(scaleReasonLabel(3), "in_flight_requests");
    assert.equal(scaleReasonLabel(4), "memory");
    assert.equal(scaleReasonLabel(5), "no_ready_instances");
  });

  await it("returns unknown(N) for unrecognized codes", () => {
    assert.equal(scaleReasonLabel(6), "unknown(6)");
    assert.equal(scaleReasonLabel(99), "unknown(99)");
  });
});

await describe("eventTypeLabel", async () => {
  await it("maps known event type codes to labels", () => {
    assert.equal(eventTypeLabel(0), "unspecified");
    assert.equal(eventTypeLabel(1), "scale up");
    assert.equal(eventTypeLabel(2), "scale down");
    assert.equal(eventTypeLabel(3), "pod assigned");
    assert.equal(eventTypeLabel(4), "pod deleted");
    assert.equal(eventTypeLabel(5), "stale replacement");
    assert.equal(eventTypeLabel(6), "heartbeat timeout");
    assert.equal(eventTypeLabel(7), "stuck instance cleanup");
  });

  await it("returns unknown(N) for unrecognized codes", () => {
    assert.equal(eventTypeLabel(99), "unknown(99)");
  });
});

await describe("eventSeverityBadge", async () => {
  await it("returns info badge for severity 1", () => {
    assert.ok(eventSeverityBadge(1).includes("info"));
    assert.ok(eventSeverityBadge(1).includes("badge-muted"));
  });

  await it("returns warn badge for severity 2", () => {
    assert.ok(eventSeverityBadge(2).includes("warn"));
    assert.ok(eventSeverityBadge(2).includes("badge-yellow"));
  });

  await it("returns unknown badge for unrecognized severity", () => {
    assert.ok(eventSeverityBadge(0).includes("unknown"));
  });
});

await describe("functionKey", async () => {
  await it("joins namespace, deployment, and tenant with colons", () => {
    assert.equal(
      functionKey({ namespace: "prod", deployment: "api", tenant: "t1" }),
      "prod:api:t1",
    );
  });

  await it("handles empty strings", () => {
    assert.equal(functionKey({ namespace: "", deployment: "", tenant: "" }), "::");
  });
});

await describe("functionPath", async () => {
  await it("returns URL-encoded path", () => {
    assert.equal(
      functionPath({ namespace: "prod", deployment: "api", tenant: "t1" }),
      "/functions/prod%3Aapi%3At1",
    );
  });
});
