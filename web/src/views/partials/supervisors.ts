import type { ClusterState } from "../../gen/types_pb.ts";
import { functionPath } from "../format.ts";

export function supervisorsTable(state: ClusterState): string {
  const supervisors = state.supervisors;
  if (supervisors.length === 0) {
    return `<div class="empty">No active supervisors</div>`;
  }

  const rows = supervisors
    .map((sup) => {
      const fn = sup.function!;
      const hash = functionPath(fn);
      const instanceCount = sup.instances.length;
      const readyCount = sup.instances.filter((i) => i.readyAt != null).length;
      const heartbeatCount = sup.routerHeartbeats.length;
      const isResponsible = sup.responsibleControllerIp === state.podIp;

      return `<tr>
      <td><a href="${hash}">${fn.deployment}</a></td>
      <td>${fn.namespace}</td>
      <td class="mono">${fn.tenant}</td>
      <td>${readyCount}/${instanceCount}</td>
      <td>${heartbeatCount}</td>
      <td><a href="/controllers/${encodeURIComponent(sup.responsibleControllerIp)}">${isResponsible ? '<span class="badge badge-green">yes</span>' : '<span class="badge badge-muted">no</span>'}</a></td>
    </tr>`;
    })
    .join("\n");

  return `<table>
  <thead>
    <tr>
      <th>Deployment</th>
      <th>Namespace</th>
      <th>Tenant</th>
      <th>Instances</th>
      <th>Heartbeats</th>
      <th>Responsible</th>
    </tr>
  </thead>
  <tbody>${rows}</tbody>
</table>`;
}

export function supervisorsPartial(state: ClusterState): Response {
  return new Response(supervisorsTable(state), {
    headers: { "content-type": "text/html; charset=utf-8" },
  });
}
