import type { ClusterState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";

export function routersPage(state: ClusterState): Response {
  const routerMap = new Map<string, { functions: number; inFlight: number }>();
  for (const sup of state.supervisors) {
    for (const hb of sup.routerHeartbeats) {
      const existing = routerMap.get(hb.routerIp);
      if (existing) {
        existing.functions++;
        existing.inFlight += hb.heartbeat?.inFlightRequests ?? 0;
      } else {
        routerMap.set(hb.routerIp, {
          functions: 1,
          inFlight: hb.heartbeat?.inFlightRequests ?? 0,
        });
      }
    }
  }

  let tableContent: string;
  if (routerMap.size === 0) {
    tableContent = `<div class="empty">No routers</div>`;
  } else {
    const rows = [...routerMap.entries()]
      .map(
        ([ip, stats]) => `<tr>
        <td><a href="/routers/${encodeURIComponent(ip)}">${ip}</a></td>
        <td>${stats.functions}</td>
        <td>${stats.inFlight}</td>
      </tr>`,
      )
      .join("\n");

    tableContent = `<table>
    <thead>
      <tr>
        <th>IP</th>
        <th>Functions</th>
        <th>Total In-Flight</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
  }

  const content = `
    <h1>Routers</h1>
    <div hx-get="/routers" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
      ${tableContent}
    </div>
  `;

  return layout("Routers", content);
}
