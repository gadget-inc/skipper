import { describe, expect, it } from "vitest";
import { makeClusterState, makeEvent } from "../test-helpers.ts";
import { eventsPage } from "./events.ts";

describe("eventsPage", () => {
  it("renders empty state", async () => {
    const state = makeClusterState({ events: [] });
    const html = await eventsPage(state).text();
    expect(html).toContain("Events");
    expect(html).toContain("No events");
  });

  it("renders events table", async () => {
    const state = makeClusterState({ events: [makeEvent({ message: "test event" })] });
    const html = await eventsPage(state).text();
    expect(html).toContain("test event");
  });

  it("includes filter controls", async () => {
    const state = makeClusterState();
    const html = await eventsPage(state).text();
    expect(html).toContain("filter-input");
    expect(html).toContain("filter-select");
    expect(html).toContain("Filter by function");
  });

  it("includes HTMX polling for events table", async () => {
    const state = makeClusterState();
    const html = await eventsPage(state).text();
    expect(html).toContain('hx-get="/partials/events"');
    expect(html).toContain('hx-trigger="every 5s"');
  });

  it("returns correct content-type", () => {
    const response = eventsPage(makeClusterState());
    expect(response.headers.get("content-type")).toBe("text/html; charset=utf-8");
  });
});
