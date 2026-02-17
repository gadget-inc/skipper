import type { Event } from "../../gen/types_pb.ts";
import {
  formatTimestamp,
  functionKey,
  functionPath,
  eventTypeLabel,
  eventSeverityBadge,
} from "../format.ts";

export function eventsTable(
  events: Event[],
  functionFilter?: string,
  severityFilter?: string,
): string {
  let filtered = events;

  if (functionFilter) {
    filtered = filtered.filter((e) => {
      if (!e.function) return false;
      return functionKey(e.function).toLowerCase().includes(functionFilter.toLowerCase());
    });
  }

  if (severityFilter && severityFilter !== "all") {
    const severityNum = parseInt(severityFilter, 10);
    if (!isNaN(severityNum)) {
      filtered = filtered.filter((e) => e.severity === severityNum);
    }
  }

  if (filtered.length === 0) {
    return `<div class="empty">No events</div>`;
  }

  const rows = filtered
    .map((event) => {
      const fnLink = event.function
        ? `<a href="${functionPath(event.function)}">${functionKey(event.function)}</a>`
        : "—";

      return `<tr>
      <td>${formatTimestamp(event.timestamp)}</td>
      <td>${fnLink}</td>
      <td>${eventTypeLabel(event.type)}</td>
      <td>${event.message}</td>
      <td>${eventSeverityBadge(event.severity)}</td>
    </tr>`;
    })
    .join("\n");

  return `<table>
  <thead>
    <tr>
      <th>Timestamp</th>
      <th>Function</th>
      <th>Type</th>
      <th>Detail</th>
      <th>Severity</th>
    </tr>
  </thead>
  <tbody>${rows}</tbody>
</table>`;
}

export function eventsPartial(
  events: Event[],
  functionFilter?: string,
  severityFilter?: string,
): Response {
  return new Response(eventsTable(events, functionFilter, severityFilter), {
    headers: { "content-type": "text/html; charset=utf-8" },
  });
}
