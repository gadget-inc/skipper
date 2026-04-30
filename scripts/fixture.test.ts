import { describe, expect, it } from "vitest";

import { formatLatency, getPercentile } from "./fixture.ts";

describe("formatLatency", () => {
  it("formats sub-millisecond values with 2 decimal places", () => {
    expect(formatLatency(0.45)).toBe("0.45ms");
    expect(formatLatency(0.01)).toBe("0.01ms");
    expect(formatLatency(0.999)).toBe("1.00ms");
  });

  it("formats 1-10ms values with 1 decimal place", () => {
    expect(formatLatency(1)).toBe("1.0ms");
    expect(formatLatency(2.34)).toBe("2.3ms");
    expect(formatLatency(9.99)).toBe("10.0ms");
  });

  it("formats 10-1000ms values as integers", () => {
    expect(formatLatency(10)).toBe("10ms");
    expect(formatLatency(45.6)).toBe("46ms");
    expect(formatLatency(999)).toBe("999ms");
  });

  it("formats values >= 1s in seconds", () => {
    expect(formatLatency(1000)).toBe("1.0s");
    expect(formatLatency(1500)).toBe("1.5s");
    expect(formatLatency(12345)).toBe("12.3s");
  });
});

describe("getPercentile", () => {
  it("returns 0 for empty array", () => {
    expect(getPercentile([], 0.5)).toBe(0);
  });

  it("returns the single element for a one-element array", () => {
    expect(getPercentile([42], 0.5)).toBe(42);
    expect(getPercentile([42], 0)).toBe(42);
    expect(getPercentile([42], 1)).toBe(42);
  });

  it("returns exact values at boundaries", () => {
    const sorted = [1, 2, 3, 4, 5];
    expect(getPercentile(sorted, 0)).toBe(1);
    expect(getPercentile(sorted, 1)).toBe(5);
  });

  it("returns the median for an odd-length array", () => {
    expect(getPercentile([1, 2, 3, 4, 5], 0.5)).toBe(3);
  });

  it("interpolates the median for an even-length array", () => {
    expect(getPercentile([1, 2, 3, 4], 0.5)).toBe(2.5);
  });

  it("interpolates fractional indices", () => {
    const sorted = [10, 20, 30, 40, 50, 60, 70, 80, 90, 100];
    // p90 = index 9 * 0.9 = 8.1 → interpolate between sorted[8]=90 and sorted[9]=100
    const p90 = getPercentile(sorted, 0.9);
    expect(p90).toBeCloseTo(91, 0);
  });

  it("handles p99 on a large array", () => {
    const sorted = Array.from({ length: 1000 }, (_, i) => i + 1);
    const p99 = getPercentile(sorted, 0.99);
    // index = 0.99 * 999 = 989.01 → between sorted[989]=990 and sorted[990]=991
    expect(p99).toBeCloseTo(990.01, 1);
  });
});
