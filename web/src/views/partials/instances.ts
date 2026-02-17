import type { Instance } from "../../gen/types_pb.ts";
import { timeAgo } from "../format.ts";

export function instancesTable(instances: Instance[], activeReplicaSet?: string): string {
  if (instances.length === 0) {
    return `<div class="empty">No instances</div>`;
  }

  const rows = instances
    .map((inst) => {
      const ready = inst.readyAt != null;
      const statusBadge = ready
        ? '<span class="badge badge-green">ready</span>'
        : '<span class="badge badge-yellow">pending</span>';

      const isStale =
        activeReplicaSet != null && activeReplicaSet !== "" && inst.replicaSet !== activeReplicaSet;
      const staleBadge = isStale ? ' <span class="badge badge-yellow">stale</span>' : "";

      return `<tr>
      <td class="mono"><a href="/instances/${encodeURIComponent(inst.name)}">${inst.name}</a></td>
      <td class="mono">${inst.addr}</td>
      <td>${statusBadge}</td>
      <td>${inst.cpuUsageMilli}m</td>
      <td>${inst.memoryUsageMib}Mi</td>
      <td class="mono">${inst.replicaSet}${staleBadge}</td>
      <td>${timeAgo(inst.assignedAt)}</td>
      <td>${ready ? timeAgo(inst.readyAt) : "—"}</td>
    </tr>`;
    })
    .join("\n");

  return `<table>
  <thead>
    <tr>
      <th>Name</th>
      <th>Address</th>
      <th>Status</th>
      <th>CPU</th>
      <th>Memory</th>
      <th>ReplicaSet</th>
      <th>Assigned</th>
      <th>Ready</th>
    </tr>
  </thead>
  <tbody>${rows}</tbody>
</table>`;
}

export function instancesPartial(instances: Instance[], activeReplicaSet?: string): Response {
  return new Response(instancesTable(instances, activeReplicaSet), {
    headers: { "content-type": "text/html; charset=utf-8" },
  });
}
