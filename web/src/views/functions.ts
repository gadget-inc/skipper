import type { ClusterState } from "../gen/types_pb.ts";
import { functionPath } from "./format.ts";
import { layout } from "./layout.ts";

export function functionsPage(state: ClusterState): Response {
  const supervisors = state.supervisors;

  let tableContent: string;
  if (supervisors.length === 0) {
    tableContent = `<div class="empty">No functions</div>`;
  } else {
    const rows = supervisors
      .map((sup) => {
        const fn = sup.function!;
        const instanceCount = sup.instances.length;
        const readyCount = sup.instances.filter((i) => i.readyAt != null).length;
        const heartbeatCount = sup.routerHeartbeats.length;
        const totalInflight = sup.routerHeartbeats.reduce(
          (sum, hb) => sum + (hb.heartbeat?.inFlightRequests ?? 0),
          0,
        );
        const scale = fn.scale;

        return `<tr>
        <td><a href="${functionPath(fn)}">${fn.deployment}</a></td>
        <td>${fn.namespace}</td>
        <td class="mono">${fn.tenant}</td>
        <td>${readyCount}/${instanceCount}</td>
        <td>${scale ? `${scale.minInstances}–${scale.maxInstances}` : "—"}</td>
        <td>${heartbeatCount}</td>
        <td>${totalInflight}</td>
        <td class="mono"><a href="/controllers/${encodeURIComponent(sup.responsibleControllerIp)}">${sup.responsibleControllerIp}</a></td>
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
        <th>Scale</th>
        <th>Routers</th>
        <th>In-Flight</th>
        <th>Responsible</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
  }

  const content = `
    <h1>Functions</h1>
    <div hx-get="/functions" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
      ${tableContent}
    </div>
  `;

  return layout("Functions", content);
}
