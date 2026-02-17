import type { ClusterState } from "../gen/types_pb.ts";
import { functionPath, timeAgo } from "./format.ts";
import { layout } from "./layout.ts";

export function routerPage(state: ClusterState, ip: string): Response {
  const entries = state.supervisors
    .filter((s) => s.routerHeartbeats.some((hb) => hb.routerIp === ip))
    .map((s) => {
      const hb = s.routerHeartbeats.find((hb) => hb.routerIp === ip)!;
      return { supervisor: s, heartbeat: hb };
    });

  if (entries.length === 0) {
    return layout("Not Found", `<div class="empty">Router not found: ${ip}</div>`);
  }

  const totalInFlight = entries.reduce(
    (sum, e) => sum + (e.heartbeat.heartbeat?.inFlightRequests ?? 0),
    0,
  );

  const rows = entries
    .map((e) => {
      const fn = e.supervisor.function!;
      return `<tr>
        <td><a href="${functionPath(fn)}">${fn.deployment}</a></td>
        <td>${fn.namespace}</td>
        <td class="mono">${fn.tenant}</td>
        <td>${e.heartbeat.heartbeat?.inFlightRequests ?? 0}</td>
        <td>${timeAgo(e.heartbeat.heartbeat?.timestamp)}</td>
      </tr>`;
    })
    .join("\n");

  const content = `
    <h1>Router ${ip}</h1>

    <div class="cards">
      <div class="card">
        <div class="label">IP Address</div>
        <div class="value" style="font-size: 16px">${ip}</div>
      </div>
      <div class="card">
        <div class="label">Functions</div>
        <div class="value">${entries.length}</div>
      </div>
      <div class="card">
        <div class="label">Total In-Flight</div>
        <div class="value">${totalInFlight}</div>
      </div>
    </div>

    <div class="section">
      <h2>Functions</h2>
      <div hx-get="/routers/${encodeURIComponent(ip)}" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
        <table>
          <thead>
            <tr>
              <th>Deployment</th>
              <th>Namespace</th>
              <th>Tenant</th>
              <th>In-Flight</th>
              <th>Last Seen</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    </div>
  `;

  return layout(`Router ${ip}`, content);
}
