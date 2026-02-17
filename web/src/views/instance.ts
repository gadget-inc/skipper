import type { ClusterState, Instance } from "../gen/types_pb.ts";
import { durationBetween, functionKey, functionPath, timeAgo } from "./format.ts";
import { layout } from "./layout.ts";

export function lifecycleTimeline(inst: Instance): string {
  const assigned = inst.assignedAt != null;
  const ready = inst.readyAt != null;

  const assignedToReady = assigned && ready ? durationBetween(inst.assignedAt, inst.readyAt) : null;
  const readyToNow = ready ? timeAgo(inst.readyAt) : null;

  // Slow startup warning if assigned→ready > 30s
  const assignedMs = inst.assignedAt ? Number(inst.assignedAt.seconds) * 1000 : 0;
  const readyMs = inst.readyAt ? Number(inst.readyAt.seconds) * 1000 : 0;
  const startupSlow = ready && readyMs - assignedMs > 30_000;

  return `<div class="timeline">
    <div class="timeline-step">
      <div class="step-dot${assigned ? "" : " inactive"}"></div>
      <div class="step-label">Assigned</div>
    </div>
    <div class="timeline-connector${assigned && ready ? " active" : ""}"></div>
    <div class="timeline-duration${startupSlow ? " slow" : ""}">${assignedToReady ?? "…"}</div>
    <div class="timeline-connector${assigned && ready ? " active" : ""}"></div>
    <div class="timeline-step">
      <div class="step-dot${ready ? "" : " inactive"}"></div>
      <div class="step-label">Ready</div>
    </div>
    <div class="timeline-connector${ready ? " active" : ""}"></div>
    <div class="timeline-duration">${readyToNow ?? "…"}</div>
    <div class="timeline-connector${ready ? " active" : ""}"></div>
    <div class="timeline-step">
      <div class="step-dot${ready ? "" : " inactive"}"></div>
      <div class="step-label">Serving</div>
    </div>
  </div>`;
}

export function instancePage(state: ClusterState, name: string): Response {
  for (const sup of state.supervisors) {
    const inst = sup.instances.find((i) => i.name === name);
    if (!inst) continue;

    const fn = sup.function!;
    const ready = inst.readyAt != null;
    const statusBadge = ready
      ? '<span class="badge badge-green">ready</span>'
      : '<span class="badge badge-yellow">pending</span>';

    const content = `
      <h1>Instance ${name} ${statusBadge}</h1>

      ${lifecycleTimeline(inst)}

      <div class="cards">
        <div class="card">
          <div class="label">Status</div>
          <div class="value">${statusBadge}</div>
        </div>
        <div class="card">
          <div class="label">Address</div>
          <div class="value" style="font-size: 16px">${inst.addr}</div>
        </div>
        <div class="card">
          <div class="label">CPU</div>
          <div class="value">${inst.cpuUsageMilli}m</div>
        </div>
        <div class="card">
          <div class="label">Memory</div>
          <div class="value">${inst.memoryUsageMib}Mi</div>
        </div>
      </div>

      <div class="section">
        <h2>Details</h2>
        <div hx-get="/instances/${encodeURIComponent(name)}" hx-trigger="every 5s" hx-swap="innerHTML" hx-select="main > *" hx-target="main">
          <table>
            <tbody>
              <tr><td>ReplicaSet</td><td class="mono">${inst.replicaSet}</td></tr>
              <tr><td>Assigned</td><td>${timeAgo(inst.assignedAt)}</td></tr>
              <tr><td>Ready</td><td>${ready ? timeAgo(inst.readyAt) : "—"}</td></tr>
              <tr><td>Function</td><td><a href="${functionPath(fn)}">${functionKey(fn)}</a></td></tr>
              <tr><td>Responsible Controller</td><td><a href="/controllers/${encodeURIComponent(sup.responsibleControllerIp)}">${sup.responsibleControllerIp}</a></td></tr>
            </tbody>
          </table>
        </div>
      </div>
    `;

    return layout(`Instance ${name}`, content);
  }

  return layout("Not Found", `<div class="empty">Instance not found: ${name}</div>`);
}
