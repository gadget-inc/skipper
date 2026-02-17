import type { ClusterState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";

export function ringDistribution(state: ClusterState): string {
  if (state.controllerIps.length === 0) return "";

  const counts: Record<string, number> = {};
  for (const ip of state.controllerIps) {
    counts[ip] = 0;
  }
  for (const sup of state.supervisors) {
    const ip = sup.responsibleControllerIp;
    if (ip in counts) {
      counts[ip]++;
    }
  }

  const max = Math.max(...Object.values(counts), 1);
  const avg = state.supervisors.length / state.controllerIps.length;
  const threshold = avg * 0.5;

  let hasImbalance = false;
  for (const count of Object.values(counts)) {
    if (avg > 0 && Math.abs(count - avg) > threshold) {
      hasImbalance = true;
      break;
    }
  }

  const bars = state.controllerIps
    .map((ip) => {
      const count = counts[ip] ?? 0;
      const pct = max > 0 ? Math.round((count / max) * 100) : 0;
      return `<div class="dist-row">
      <div class="dist-label">${ip}</div>
      <div class="dist-bar-bg"><div class="dist-bar" style="width: ${pct}%"></div></div>
      <div class="dist-count">${count}</div>
    </div>`;
    })
    .join("\n");

  const imbalanceBadge = hasImbalance ? ' <span class="badge badge-yellow">imbalanced</span>' : "";

  return `<div class="section">
    <h2>Ring Distribution${imbalanceBadge}</h2>
    <div class="dist-chart">${bars}</div>
  </div>`;
}

export function controllersPage(state: ClusterState): Response {
  let tableContent: string;
  if (state.controllerIps.length === 0) {
    tableContent = `<div class="empty">No controllers</div>`;
  } else {
    const rows = state.controllerIps
      .map((ip) => {
        const isSelf = ip === state.podIp;
        const supervisors = state.supervisors.filter((s) => s.responsibleControllerIp === ip);
        const totalInstances = supervisors.reduce((sum, s) => sum + s.instances.length, 0);
        const readyInstances = supervisors.reduce(
          (sum, s) => sum + s.instances.filter((i) => i.readyAt != null).length,
          0,
        );

        return `<tr>
        <td><a href="/controllers/${encodeURIComponent(ip)}">${ip}</a></td>
        <td>${isSelf ? '<span class="badge badge-green">self</span>' : ""}</td>
        <td>${supervisors.length}</td>
        <td>${readyInstances}/${totalInstances}</td>
      </tr>`;
      })
      .join("\n");

    tableContent = `<table>
    <thead>
      <tr>
        <th>IP</th>
        <th>Self</th>
        <th>Functions</th>
        <th>Instances</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
  }

  const content = `
    <h1>Controllers</h1>
    ${ringDistribution(state)}
    <div hx-get="/controllers" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
      ${tableContent}
    </div>
  `;

  return layout("Controllers", content);
}
