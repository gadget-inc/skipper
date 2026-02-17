import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeEvent, makeFunction } from "../../test-helpers.ts";
import { recentActivityWidget } from "./recent-activity.ts";

await describe("recentActivityWidget", async () => {
  await it("shows empty message for no events", () => {
    const html = recentActivityWidget([]);
    assert.ok(html.includes("No recent activity"));
  });

  await it("shows at most 5 events", () => {
    const events = Array.from({ length: 10 }, (_, i) => makeEvent({ message: `event-${i}` }));
    const html = recentActivityWidget(events);
    assert.ok(html.includes("event-0"));
    assert.ok(html.includes("event-4"));
    assert.ok(!html.includes("event-5"));
  });

  await it("links to all events page", () => {
    const html = recentActivityWidget([makeEvent()]);
    assert.ok(html.includes('href="/events"'));
    assert.ok(html.includes("View all events"));
  });

  await it("renders event details", () => {
    const fn = makeFunction({ deployment: "my-api" });
    const event = makeEvent({ function: fn, message: "scaled up" });
    const html = recentActivityWidget([event]);
    assert.ok(html.includes("my-api"));
    assert.ok(html.includes("scaled up"));
  });
});
