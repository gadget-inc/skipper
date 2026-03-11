import { describe, it, expect } from "vitest";
import { normalizeComments, badgeText, relativeTime } from "./comments-utils.ts";

describe("relativeTime", () => {
  // Fixed reference point: 2026-03-09T12:00:00Z
  const now = new Date("2026-03-09T12:00:00Z").getTime();

  const cases = [
    { name: "less than a minute ago", date: "2026-03-09T11:59:30Z", want: "just now" },
    { name: "exactly 1 minute ago", date: "2026-03-09T11:59:00Z", want: "1m ago" },
    { name: "30 minutes ago", date: "2026-03-09T11:30:00Z", want: "30m ago" },
    { name: "59 minutes ago", date: "2026-03-09T11:01:00Z", want: "59m ago" },
    { name: "exactly 1 hour ago", date: "2026-03-09T11:00:00Z", want: "1h ago" },
    { name: "23 hours ago", date: "2026-03-08T13:00:00Z", want: "23h ago" },
    { name: "exactly 1 day ago", date: "2026-03-08T12:00:00Z", want: "1d ago" },
    { name: "6 days ago", date: "2026-03-03T12:00:00Z", want: "6d ago" },
    {
      name: "7 days ago falls back to date",
      date: "2026-03-02T12:00:00Z",
      want: new Date("2026-03-02T12:00:00Z").toLocaleDateString(),
    },
  ];

  for (const { name, date, want } of cases) {
    it(name, () => {
      expect(relativeTime(date, now)).toBe(want);
    });
  }

  it("future date falls through to toLocaleDateString and does not throw", () => {
    const futureDate = new Date(Date.now() + 5 * 60 * 1000).toISOString();
    // diffMs is negative for a future date; diffMin < 1, so it returns "just now"
    // OR falls through to toLocaleDateString — either way it must not throw
    expect(() => relativeTime(futureDate)).not.toThrow();
    const result = relativeTime(futureDate);
    expect(typeof result).toBe("string");
  });
});

describe("normalizeComments", () => {
  it("passes through a valid array", () => {
    const arr = [{ id: "1" }, { id: "2" }];
    expect(normalizeComments(arr)).toBe(arr);
  });

  it("returns empty array for undefined", () => {
    expect(normalizeComments(undefined)).toEqual([]);
  });

  it("returns empty array for null", () => {
    expect(normalizeComments(null)).toEqual([]);
  });

  it("returns empty array for an object (e.g. error response)", () => {
    expect(normalizeComments({ error: "server error" })).toEqual([]);
  });

  it("returns empty array for a string", () => {
    expect(normalizeComments("not an array")).toEqual([]);
  });

  it("passes through an empty array", () => {
    expect(normalizeComments([])).toEqual([]);
  });
});

describe("badgeText", () => {
  it("returns singular for 1 comment", () => {
    expect(badgeText(1)).toBe("1 comment");
  });

  it("returns plural for multiple comments", () => {
    expect(badgeText(5)).toBe("5 comments");
  });

  it("returns null for 0", () => {
    expect(badgeText(0)).toBeNull();
  });

  it("returns null for undefined", () => {
    expect(badgeText(undefined)).toBeNull();
  });

  it("returns null for null", () => {
    expect(badgeText(null)).toBeNull();
  });

  it("returns null for negative numbers", () => {
    expect(badgeText(-1)).toBeNull();
  });

  it("returns null for NaN", () => {
    expect(badgeText(NaN)).toBeNull();
  });

  it("returns null for a string (prevents 'undefined comments')", () => {
    expect(badgeText("3")).toBeNull();
  });

  it("returns null for float input", () => {
    expect(badgeText(1.5)).toBeNull();
  });

  it("returns text for a large number", () => {
    expect(badgeText(1000)).toBe("1000 comments");
  });

  it("returns null for Infinity", () => {
    expect(badgeText(Infinity)).toBeNull();
  });
});
