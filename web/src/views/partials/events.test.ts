import { describe, expect, it } from "vitest";
import { makeEvent, makeFunction } from "../../test-helpers.ts";
import { eventsTable } from "./events.ts";

describe("eventsTable", () => {
  it("shows empty message for no events", () => {
    const html = eventsTable([]);
    expect(html).toContain("No events");
    expect(html).toContain('class="empty"');
  });

  it("renders event rows with all fields", () => {
    const fn = makeFunction({ deployment: "api-server" });
    const event = makeEvent({ function: fn, type: 1, message: "scaled up to 3", severity: 1 });
    const html = eventsTable([event]);
    expect(html).toContain("api-server");
    expect(html).toContain("scale up");
    expect(html).toContain("scaled up to 3");
    expect(html).toContain("info");
  });

  it("renders function link", () => {
    const event = makeEvent();
    const html = eventsTable([event]);
    expect(html).toContain('href="/functions/');
  });

  it("renders severity badge", () => {
    const warnEvent = makeEvent({ severity: 2 });
    const html = eventsTable([warnEvent]);
    expect(html).toContain("badge-yellow");
    expect(html).toContain("warn");
  });

  it("filters by function name", () => {
    const fn1 = makeFunction({ deployment: "api" });
    const fn2 = makeFunction({ deployment: "worker" });
    const e1 = makeEvent({ function: fn1, message: "event1" });
    const e2 = makeEvent({ function: fn2, message: "event2" });
    const html = eventsTable([e1, e2], "worker");
    expect(html).not.toContain("event1");
    expect(html).toContain("event2");
  });

  it("filters by severity", () => {
    const e1 = makeEvent({ severity: 1, message: "info event" });
    const e2 = makeEvent({ severity: 2, message: "warn event" });
    const html = eventsTable([e1, e2], undefined, "2");
    expect(html).not.toContain("info event");
    expect(html).toContain("warn event");
  });

  it("shows empty when all events filtered out", () => {
    const event = makeEvent({ severity: 1 });
    const html = eventsTable([event], undefined, "2");
    expect(html).toContain("No events");
  });
});
