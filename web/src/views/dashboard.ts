import type { ClusterState } from "../gen/types_pb.ts";
import { layout } from "./layout.ts";
import { timeAgo } from "./format.ts";
import { supervisorsTable } from "./partials/supervisors.ts";
import { healthIndicators } from "./partials/health.ts";
import { recentActivityWidget } from "./partials/recent-activity.ts";

export function dashboardPage(state: ClusterState): Response {
  const totalFunctions = state.supervisors.length;
  const totalInstances = state.supervisors.reduce((sum, sup) => sum + sup.instances.length, 0);
  const readyInstances = state.supervisors.reduce(
    (sum, sup) => sum + sup.instances.filter((i) => i.readyAt != null).length,
    0,
  );
  const controllerCount = state.controllerIps.length;

  const content = `
    <h1>Dashboard</h1>

    <div hx-get="/partials/health" hx-trigger="every 5s" hx-swap="innerHTML">
      ${healthIndicators(state)}
    </div>

    <div class="cards">
      <div class="card">
        <div class="label">Controllers</div>
        <div class="value">${controllerCount}</div>
      </div>
      <div class="card">
        <div class="label">Functions</div>
        <div class="value">${totalFunctions}</div>
      </div>
      <div class="card">
        <div class="label">Instances</div>
        <div class="value">${readyInstances}/${totalInstances}</div>
      </div>
      <div class="card">
        <div class="label">Uptime</div>
        <div class="value" style="font-size: 20px">${timeAgo(state.startedAt)}</div>
      </div>
    </div>

    <div class="section">
      <h2>Controller Ring</h2>
      <p class="mono" style="color: var(--text-muted); font-size: 13px; margin-bottom: 16px">
        ${state.controllerIps.map((ip) => `<a href="/controllers/${encodeURIComponent(ip)}">${ip}</a>`).join(" · ")}
        <span style="margin-left: 8px">(self: ${state.podIp})</span>
      </p>
    </div>

    <div class="section">
      <h2>Supervisors</h2>
      <div hx-get="/partials/supervisors" hx-trigger="every 5s" hx-swap="innerHTML">
        ${supervisorsTable(state)}
      </div>
    </div>

    <div class="section">
      <h2>Recent Activity</h2>
      <div hx-get="/partials/recent-activity" hx-trigger="every 5s" hx-swap="innerHTML">
        ${recentActivityWidget(state.events)}
      </div>
    </div>
  `;

  return layout("Dashboard", content);
}
