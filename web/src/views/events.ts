import type { ClusterState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";
import { eventsTable } from "./partials/events.ts";

export function eventsPage(
  state: ClusterState,
  functionFilter?: string,
  severityFilter?: string,
): Response {
  const content = `
    <h1>Events</h1>

    <div class="filter-bar">
      <input
        type="text"
        name="function"
        class="filter-input"
        placeholder="Filter by function…"
        value="${functionFilter ?? ""}"
        hx-get="/partials/events"
        hx-trigger="keyup changed delay:300ms"
        hx-target="#events-table"
        hx-include="[name='severity']"
      />
      <select
        name="severity"
        class="filter-select"
        hx-get="/partials/events"
        hx-trigger="change"
        hx-target="#events-table"
        hx-include="[name='function']"
      >
        <option value="all"${!severityFilter || severityFilter === "all" ? " selected" : ""}>All severities</option>
        <option value="1"${severityFilter === "1" ? " selected" : ""}>Info</option>
        <option value="2"${severityFilter === "2" ? " selected" : ""}>Warn</option>
      </select>
    </div>

    <div class="section">
      <div id="events-table" hx-get="/partials/events" hx-trigger="every 5s" hx-include="[name='function'],[name='severity']">
        ${eventsTable(state.events, functionFilter, severityFilter)}
      </div>
    </div>
  `;

  return layout("Events", content);
}
