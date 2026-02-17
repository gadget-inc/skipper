import type { Timestamp } from "@bufbuild/protobuf/wkt";

export function timeAgo(ts: Timestamp | undefined): string {
  if (!ts) return "—";
  const seconds = Math.floor((Date.now() - Number(ts.seconds) * 1000) / 1000);
  if (seconds < 0) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function formatTimestamp(ts: Timestamp | undefined): string {
  if (!ts) return "—";
  return new Date(Number(ts.seconds) * 1000).toISOString();
}

export function durationBetween(start: Timestamp | undefined, end: Timestamp | undefined): string {
  if (!start || !end) return "—";
  const ms = Number(end.seconds) * 1000 - Number(start.seconds) * 1000;
  if (ms < 0) return "—";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (remainingSeconds === 0) return `${minutes}m`;
  return `${minutes}m ${remainingSeconds}s`;
}

export function functionKey(fn: { namespace: string; deployment: string; tenant: string }): string {
  return `${fn.namespace}:${fn.deployment}:${fn.tenant}`;
}

export function functionPath(fn: {
  namespace: string;
  deployment: string;
  tenant: string;
}): string {
  return `/functions/${encodeURIComponent(functionKey(fn))}`;
}

export function scaleReasonLabel(reason: number): string {
  switch (reason) {
    case 0:
      return "unspecified";
    case 1:
      return "cpu";
    case 2:
      return "heartbeat_timeout";
    case 3:
      return "in_flight_requests";
    case 4:
      return "memory";
    case 5:
      return "no_ready_instances";
    default:
      return `unknown(${reason})`;
  }
}

export function eventTypeLabel(type_: number): string {
  switch (type_) {
    case 0:
      return "unspecified";
    case 1:
      return "scale up";
    case 2:
      return "scale down";
    case 3:
      return "pod assigned";
    case 4:
      return "pod deleted";
    case 5:
      return "stale replacement";
    case 6:
      return "heartbeat timeout";
    case 7:
      return "stuck instance cleanup";
    default:
      return `unknown(${type_})`;
  }
}

export function eventSeverityBadge(severity: number): string {
  switch (severity) {
    case 1:
      return '<span class="badge badge-muted">info</span>';
    case 2:
      return '<span class="badge badge-yellow">warn</span>';
    default:
      return '<span class="badge badge-muted">unknown</span>';
  }
}
