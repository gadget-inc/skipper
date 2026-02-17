import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { makeEvent, makeFunction } from "../../test-helpers.ts";
import { eventsTable } from "./events.ts";

await describe("eventsTable", async () => {
  await it("shows empty message for no events", () => {
    const html = eventsTable([]);
    assert.ok(html.includes("No events"));
    assert.ok(html.includes('class="empty"'));
  });

  await it("renders event rows with all fields", () => {
    const fn = makeFunction({ deployment: "api-server" });
    const event = makeEvent({ function: fn, type: 1, message: "scaled up to 3", severity: 1 });
    const html = eventsTable([event]);
    assert.ok(html.includes("api-server"));
    assert.ok(html.includes("scale up"));
    assert.ok(html.includes("scaled up to 3"));
    assert.ok(html.includes("info"));
  });

  await it("renders function link", () => {
    const event = makeEvent();
    const html = eventsTable([event]);
    assert.ok(html.includes('href="/functions/'));
  });

  await it("renders severity badge", () => {
    const warnEvent = makeEvent({ severity: 2 });
    const html = eventsTable([warnEvent]);
    assert.ok(html.includes("badge-yellow"));
    assert.ok(html.includes("warn"));
  });

  await it("filters by function name", () => {
    const fn1 = makeFunction({ deployment: "api" });
    const fn2 = makeFunction({ deployment: "worker" });
    const e1 = makeEvent({ function: fn1, message: "event1" });
    const e2 = makeEvent({ function: fn2, message: "event2" });
    const html = eventsTable([e1, e2], "worker");
    assert.ok(!html.includes("event1"));
    assert.ok(html.includes("event2"));
  });

  await it("filters by severity", () => {
    const e1 = makeEvent({ severity: 1, message: "info event" });
    const e2 = makeEvent({ severity: 2, message: "warn event" });
    const html = eventsTable([e1, e2], undefined, "2");
    assert.ok(!html.includes("info event"));
    assert.ok(html.includes("warn event"));
  });

  await it("shows empty when all events filtered out", () => {
    const event = makeEvent({ severity: 1 });
    const html = eventsTable([event], undefined, "2");
    assert.ok(html.includes("No events"));
  });
});
