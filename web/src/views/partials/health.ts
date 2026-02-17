import type { ClusterState } from "../../gen/types_pb.ts";

export interface HealthIssue {
  color: "red" | "yellow";
  label: string;
  count: number;
  link: string;
}

export function computeHealthIssues(state: ClusterState): HealthIssue[] {
  const issues: HealthIssue[] = [];

  // Stuck instances: assigned but not ready > 60s
  let stuckCount = 0;
  for (const sup of state.supervisors) {
    for (const inst of sup.instances) {
      if (inst.readyAt == null && inst.assignedAt != null) {
        const assignedMs = Number(inst.assignedAt.seconds) * 1000;
        if (Date.now() - assignedMs > 60_000) {
          stuckCount++;
        }
      }
    }
  }
  if (stuckCount > 0) {
    issues.push({ color: "red", label: "Stuck instances", count: stuckCount, link: "/functions" });
  }

  // Functions waiting for pods: has instances but none ready
  let waitingCount = 0;
  for (const sup of state.supervisors) {
    if (sup.instances.length > 0 && sup.instances.every((i) => i.readyAt == null)) {
      waitingCount++;
    }
  }
  if (waitingCount > 0) {
    issues.push({
      color: "yellow",
      label: "Functions waiting for pods",
      count: waitingCount,
      link: "/functions",
    });
  }

  // Stale heartbeats: > 60s old (approaching 90s timeout)
  let staleHeartbeatCount = 0;
  for (const sup of state.supervisors) {
    for (const hb of sup.routerHeartbeats) {
      if (hb.heartbeat?.timestamp != null) {
        const hbMs = Number(hb.heartbeat.timestamp.seconds) * 1000;
        if (Date.now() - hbMs > 60_000) {
          staleHeartbeatCount++;
        }
      }
    }
  }
  if (staleHeartbeatCount > 0) {
    issues.push({
      color: "yellow",
      label: "Stale heartbeats",
      count: staleHeartbeatCount,
      link: "/routers",
    });
  }

  // Stale instances: on non-active replica sets
  let staleInstanceCount = 0;
  for (const sup of state.supervisors) {
    if (sup.activeReplicaSet) {
      for (const inst of sup.instances) {
        if (inst.replicaSet !== sup.activeReplicaSet) {
          staleInstanceCount++;
        }
      }
    }
  }
  if (staleInstanceCount > 0) {
    issues.push({
      color: "yellow",
      label: "Stale instances",
      count: staleInstanceCount,
      link: "/functions",
    });
  }

  return issues;
}

export function healthIndicators(state: ClusterState): string {
  const issues = computeHealthIssues(state);

  if (issues.length === 0) {
    return `<div class="health-indicators">
      <div class="health-indicator">
        <div class="health-dot health-dot-green"></div>
        <span>All healthy</span>
      </div>
    </div>`;
  }

  const indicators = issues
    .map(
      (
        issue,
      ) => `<a href="${issue.link}" class="health-indicator" style="text-decoration: none; color: inherit">
      <div class="health-dot health-dot-${issue.color}"></div>
      <span>${issue.label}</span>
      <span class="badge badge-${issue.color}">${issue.count}</span>
    </a>`,
    )
    .join("\n");

  return `<div class="health-indicators">${indicators}</div>`;
}

export function healthPartial(state: ClusterState): Response {
  return new Response(healthIndicators(state), {
    headers: { "content-type": "text/html; charset=utf-8" },
  });
}
