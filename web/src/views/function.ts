import type { ClusterState, ScaleDecision, SupervisorState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";
import { formatTimestamp, functionKey, scaleReasonLabel, timeAgo } from "./format.ts";
import { instancesTable } from "./partials/instances.ts";

export function functionPage(state: ClusterState, key: string): Response {
  const sup = findSupervisor(state, key);
  if (!sup) {
    return layout("Not Found", `<div class="empty">Function not found: ${key}</div>`);
  }

  const fn = sup.function!;
  const scale = fn.scale;
  const lastDecision = sup.lastScaleDecision;
  const encodedKey = encodeURIComponent(key);

  const content = `
    <h1>${fn.deployment} <span style="color: var(--text-muted); font-weight: 400">/ ${fn.tenant}</span></h1>

    <div class="cards">
      <div class="card">
        <div class="label">Namespace</div>
        <div class="value" style="font-size: 16px">${fn.namespace}</div>
      </div>
      <div class="card">
        <div class="label">Instances</div>
        <div class="value">${sup.instances.filter((i) => i.readyAt != null).length}/${sup.instances.length}</div>
      </div>
      <div class="card">
        <div class="label">Scale Range</div>
        <div class="value">${scale ? `${scale.minInstances}–${scale.maxInstances}` : "—"}</div>
      </div>
      <div class="card">
        <div class="label">Responsible</div>
        <div class="value" style="font-size: 14px"><a href="/controllers/${encodeURIComponent(sup.responsibleControllerIp)}">${sup.responsibleControllerIp}</a></div>
      </div>
    </div>

    ${scale ? scaleConfigSection(scale) : ""}

    ${lastDecision ? lastDecisionSection(lastDecision) : ""}

    ${scalingHistorySection(sup.scaleHistory)}

    <div class="section">
      <h2>Router Heartbeats</h2>
      ${heartbeatsSection(sup)}
    </div>

    ${rolloutBanner(sup)}

    <div class="section">
      <h2>Instances</h2>
      <div hx-get="/partials/instances/${encodedKey}" hx-trigger="every 5s" hx-swap="innerHTML">
        ${instancesTable(sup.instances, sup.activeReplicaSet)}
      </div>
    </div>
  `;

  return layout(fn.deployment, content);
}

function scaleConfigSection(scale: NonNullable<ReturnType<typeof Object.create>>): string {
  return `
    <div class="section">
      <h2>Scale Configuration</h2>
      <table>
        <thead>
          <tr><th>Parameter</th><th>Value</th></tr>
        </thead>
        <tbody>
          <tr><td>Target CPU</td><td>${scale.targetCpuUsageMilli}m</td></tr>
          <tr><td>Target Memory</td><td>${scale.targetMemoryUsageMib}Mi</td></tr>
          <tr><td>Target In-Flight Requests</td><td>${scale.targetInFlightRequests}</td></tr>
        </tbody>
      </table>
    </div>
  `;
}

function lastDecisionSection(decision: NonNullable<ReturnType<typeof Object.create>>): string {
  return `
    <div class="section">
      <h2>Last Scale Decision</h2>
      <table>
        <thead>
          <tr><th>Desired</th><th>Unclamped</th><th>Reason</th></tr>
        </thead>
        <tbody>
          <tr>
            <td>${decision.desiredInstances}</td>
            <td>${decision.unclampedDesiredInstances}</td>
            <td><span class="badge badge-muted">${scaleReasonLabel(decision.reason)}</span></td>
          </tr>
        </tbody>
      </table>
    </div>
  `;
}

export function scalingHistorySection(history: ScaleDecision[]): string {
  if (history.length === 0) {
    return "";
  }

  const rows = history
    .slice()
    .reverse()
    .map((decision) => {
      const clamped = decision.desiredInstances !== decision.unclampedDesiredInstances;
      const rowClass = clamped ? ' class="row-highlight"' : "";
      const metricsStr = decision.metrics.map((m) => `${m.name}: ${m.value.toFixed(1)}`).join(", ");

      return `<tr${rowClass}>
      <td>${formatTimestamp(decision.timestamp)}</td>
      <td>${decision.desiredInstances}${clamped ? " (clamped)" : ""}</td>
      <td>${decision.unclampedDesiredInstances}</td>
      <td><span class="badge badge-muted">${scaleReasonLabel(decision.reason)}</span></td>
      <td style="color: var(--text-muted)">${metricsStr || "—"}</td>
    </tr>`;
    })
    .join("\n");

  return `
    <div class="section">
      <h2>Scaling History</h2>
      <table>
        <thead>
          <tr>
            <th>Timestamp</th>
            <th>Desired</th>
            <th>Unclamped</th>
            <th>Reason</th>
            <th>Metrics</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}

function rolloutBanner(sup: SupervisorState): string {
  if (!sup.activeReplicaSet) return "";

  const staleInstances = sup.instances.filter((i) => i.replicaSet !== sup.activeReplicaSet);
  const currentInstances = sup.instances.filter((i) => i.replicaSet === sup.activeReplicaSet);

  if (staleInstances.length === 0) return "";

  return `<div class="banner banner-yellow">
    Rollout in progress — ${staleInstances.length} stale / ${currentInstances.length} current instances
  </div>`;
}

function heartbeatsSection(sup: SupervisorState): string {
  if (sup.routerHeartbeats.length === 0) {
    return `<div class="empty">No router heartbeats</div>`;
  }

  const rows = sup.routerHeartbeats
    .map(
      (hb) => `<tr>
      <td class="mono"><a href="/routers/${encodeURIComponent(hb.routerIp)}">${hb.routerIp}</a></td>
      <td>${hb.heartbeat?.inFlightRequests ?? 0}</td>
      <td>${timeAgo(hb.heartbeat?.timestamp)}</td>
    </tr>`,
    )
    .join("\n");

  return `<table>
    <thead>
      <tr><th>Router IP</th><th>In-Flight</th><th>Last Seen</th></tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>`;
}

function findSupervisor(state: ClusterState, key: string): SupervisorState | undefined {
  return state.supervisors.find((sup) => {
    const fn = sup.function;
    return fn != null && functionKey(fn) === key;
  });
}
