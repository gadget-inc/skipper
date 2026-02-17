import type { Event } from "../../gen/types_pb.ts";
import {
  formatTimestamp,
  functionKey,
  functionPath,
  eventTypeLabel,
  eventSeverityBadge,
} from "../format.ts";

export function recentActivityWidget(events: Event[]): string {
  const recent = events.slice(0, 5);

  if (recent.length === 0) {
    return `<div class="empty">No recent activity</div>`;
  }

  const rows = recent
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
</table>
<p style="margin-top: 8px; font-size: 13px"><a href="/events">View all events →</a></p>`;
}

export function recentActivityPartial(events: Event[]): Response {
  return new Response(recentActivityWidget(events), {
    headers: { "content-type": "text/html; charset=utf-8" },
  });
}
