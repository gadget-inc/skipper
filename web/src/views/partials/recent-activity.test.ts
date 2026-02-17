import { describe, expect, it } from "vitest";
import { makeEvent, makeFunction } from "../../test-helpers.ts";
import { recentActivityWidget } from "./recent-activity.ts";

describe("recentActivityWidget", () => {
  it("shows empty message for no events", () => {
    const html = recentActivityWidget([]);
    expect(html).toContain("No recent activity");
  });

  it("shows at most 5 events", () => {
    const events = Array.from({ length: 10 }, (_, i) => makeEvent({ message: `event-${i}` }));
    const html = recentActivityWidget(events);
    expect(html).toContain("event-0");
    expect(html).toContain("event-4");
    expect(html).not.toContain("event-5");
  });

  it("links to all events page", () => {
    const html = recentActivityWidget([makeEvent()]);
    expect(html).toContain('href="/events"');
    expect(html).toContain("View all events");
  });

  it("renders event details", () => {
    const fn = makeFunction({ deployment: "my-api" });
    const event = makeEvent({ function: fn, message: "scaled up" });
    const html = recentActivityWidget([event]);
    expect(html).toContain("my-api");
    expect(html).toContain("scaled up");
  });
});
