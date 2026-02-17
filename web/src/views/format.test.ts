import { describe, expect, it, vi } from "vitest";
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

describe("timeAgo", () => {
  it("returns dash for undefined", () => {
    expect(timeAgo(undefined)).toBe("—");
  });

  it("returns 'just now' for future timestamps", () => {
    const ts = makeTimestamp(60_000);
    expect(timeAgo(ts)).toBe("just now");
  });

  it("returns seconds for <60s", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const ts = makeTimestamp(-30_000);
    expect(timeAgo(ts)).toBe("30s ago");

    vi.useRealTimers();
  });

  it("returns minutes for <60m", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const ts = makeTimestamp(-5 * 60_000);
    expect(timeAgo(ts)).toBe("5m ago");

    vi.useRealTimers();
  });

  it("returns hours for <24h", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const ts = makeTimestamp(-3 * 3_600_000);
    expect(timeAgo(ts)).toBe("3h ago");

    vi.useRealTimers();
  });

  it("returns days for >=24h", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const ts = makeTimestamp(-2 * 86_400_000);
    expect(timeAgo(ts)).toBe("2d ago");

    vi.useRealTimers();
  });

  it("returns 0s ago for exactly now", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.now());

    const ts = makeTimestamp(0);
    expect(timeAgo(ts)).toBe("0s ago");

    vi.useRealTimers();
  });
});

describe("formatTimestamp", () => {
  it("returns dash for undefined", () => {
    expect(formatTimestamp(undefined)).toBe("—");
  });

  it("returns ISO string for a known timestamp", () => {
    const ts = makeTimestamp(0);
    const result = formatTimestamp(ts);
    // Should be a valid ISO 8601 string
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });
});

describe("durationBetween", () => {
  it("returns dash for undefined start", () => {
    expect(durationBetween(undefined, makeTimestamp(0))).toBe("—");
  });

  it("returns dash for undefined end", () => {
    expect(durationBetween(makeTimestamp(0), undefined)).toBe("—");
  });

  it("returns seconds for short durations", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1005000);
    expect(durationBetween(start, end)).toBe("5s");
  });

  it("returns minutes and seconds for longer durations", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1090000);
    expect(durationBetween(start, end)).toBe("1m 30s");
  });

  it("returns minutes only when no remaining seconds", () => {
    const start = timestampFromMs(1000000);
    const end = timestampFromMs(1120000);
    expect(durationBetween(start, end)).toBe("2m");
  });

  it("returns 0s for zero duration", () => {
    const ts = timestampFromMs(1000000);
    expect(durationBetween(ts, ts)).toBe("0s");
  });
});

describe("scaleReasonLabel", () => {
  it("maps known reason codes to labels", () => {
    expect(scaleReasonLabel(0)).toBe("unspecified");
    expect(scaleReasonLabel(1)).toBe("cpu");
    expect(scaleReasonLabel(2)).toBe("heartbeat_timeout");
    expect(scaleReasonLabel(3)).toBe("in_flight_requests");
    expect(scaleReasonLabel(4)).toBe("memory");
    expect(scaleReasonLabel(5)).toBe("no_ready_instances");
  });

  it("returns unknown(N) for unrecognized codes", () => {
    expect(scaleReasonLabel(6)).toBe("unknown(6)");
    expect(scaleReasonLabel(99)).toBe("unknown(99)");
  });
});

describe("eventTypeLabel", () => {
  it("maps known event type codes to labels", () => {
    expect(eventTypeLabel(0)).toBe("unspecified");
    expect(eventTypeLabel(1)).toBe("scale up");
    expect(eventTypeLabel(2)).toBe("scale down");
    expect(eventTypeLabel(3)).toBe("pod assigned");
    expect(eventTypeLabel(4)).toBe("pod deleted");
    expect(eventTypeLabel(5)).toBe("stale replacement");
    expect(eventTypeLabel(6)).toBe("heartbeat timeout");
    expect(eventTypeLabel(7)).toBe("stuck instance cleanup");
  });

  it("returns unknown(N) for unrecognized codes", () => {
    expect(eventTypeLabel(99)).toBe("unknown(99)");
  });
});

describe("eventSeverityBadge", () => {
  it("returns info badge for severity 1", () => {
    expect(eventSeverityBadge(1)).toContain("info");
    expect(eventSeverityBadge(1)).toContain("badge-muted");
  });

  it("returns warn badge for severity 2", () => {
    expect(eventSeverityBadge(2)).toContain("warn");
    expect(eventSeverityBadge(2)).toContain("badge-yellow");
  });

  it("returns unknown badge for unrecognized severity", () => {
    expect(eventSeverityBadge(0)).toContain("unknown");
  });
});

describe("functionKey", () => {
  it("joins namespace, deployment, and tenant with colons", () => {
    expect(functionKey({ namespace: "prod", deployment: "api", tenant: "t1" })).toBe("prod:api:t1");
  });

  it("handles empty strings", () => {
    expect(functionKey({ namespace: "", deployment: "", tenant: "" })).toBe("::");
  });
});

describe("functionPath", () => {
  it("returns URL-encoded path", () => {
    expect(functionPath({ namespace: "prod", deployment: "api", tenant: "t1" })).toBe(
      "/functions/prod%3Aapi%3At1",
    );
  });
});
