import type { ClusterState } from "../gen/types_pb.ts";
import { functionPath, timeAgo } from "./format.ts";
import { layout } from "./layout.ts";

export function controllerPage(state: ClusterState, ip: string): Response {
  if (!state.controllerIps.includes(ip)) {
    return layout("Not Found", `<div class="empty">Controller not found: ${ip}</div>`);
  }

  const isSelf = ip === state.podIp;
  const supervisors = state.supervisors.filter((s) => s.responsibleControllerIp === ip);
  const totalInstances = supervisors.reduce((sum, s) => sum + s.instances.length, 0);
  const readyInstances = supervisors.reduce(
    (sum, s) => sum + s.instances.filter((i) => i.readyAt != null).length,
    0,
  );

  let tableContent: string;
  if (supervisors.length === 0) {
    tableContent = `<div class="empty">No functions assigned</div>`;
  } else {
    const rows = supervisors
      .map((sup) => {
        const fn = sup.function!;
        const readyCount = sup.instances.filter((i) => i.readyAt != null).length;
        return `<tr>
        <td><a href="${functionPath(fn)}">${fn.deployment}</a></td>
        <td>${fn.namespace}</td>
        <td class="mono">${fn.tenant}</td>
        <td>${readyCount}/${sup.instances.length}</td>
        <td>${sup.routerHeartbeats.length}</td>
      </tr>`;
      })
      .join("\n");

    tableContent = `<table>
    <thead>
      <tr>
        <th>Deployment</th>
        <th>Namespace</th>
        <th>Tenant</th>
        <th>Instances</th>
        <th>Heartbeats</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
  }

  const content = `
    <h1>Controller ${ip} ${isSelf ? '<span class="badge badge-green">self</span>' : ""}</h1>

    <div class="cards">
      <div class="card">
        <div class="label">IP Address</div>
        <div class="value" style="font-size: 16px">${ip}</div>
      </div>
      <div class="card">
        <div class="label">Functions</div>
        <div class="value">${supervisors.length}</div>
      </div>
      <div class="card">
        <div class="label">Instances</div>
        <div class="value">${readyInstances}/${totalInstances}</div>
      </div>
      <div class="card">
        <div class="label">Uptime</div>
        <div class="value" style="font-size: 20px">${isSelf ? timeAgo(state.startedAt) : "—"}</div>
      </div>
    </div>

    <div class="section">
      <h2>Functions</h2>
      <div hx-get="/controllers/${encodeURIComponent(ip)}" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
        ${tableContent}
      </div>
    </div>
  `;

  return layout(`Controller ${ip}`, content);
}
