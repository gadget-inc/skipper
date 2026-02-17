import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeClusterState, makeEvent } from "../test-helpers.ts";
import { eventsPage } from "./events.ts";

await describe("eventsPage", async () => {
  await it("renders empty state", async () => {
    const state = makeClusterState({ events: [] });
    const html = await eventsPage(state).text();
    assert.ok(html.includes("Events"));
    assert.ok(html.includes("No events"));
  });

  await it("renders events table", async () => {
    const state = makeClusterState({ events: [makeEvent({ message: "test event" })] });
    const html = await eventsPage(state).text();
    assert.ok(html.includes("test event"));
  });

  await it("includes filter controls", async () => {
    const state = makeClusterState();
    const html = await eventsPage(state).text();
    assert.ok(html.includes("filter-input"));
    assert.ok(html.includes("filter-select"));
    assert.ok(html.includes("Filter by function"));
  });

  await it("includes HTMX polling for events table", async () => {
    const state = makeClusterState();
    const html = await eventsPage(state).text();
    assert.ok(html.includes('hx-get="/partials/events"'));
    assert.ok(html.includes('hx-trigger="every 5s"'));
  });

  await it("returns correct content-type", () => {
    const response = eventsPage(makeClusterState());
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  });
});
