/** @fileoverview Pure utility functions for the comments system. */

/**
 * Return a relative time string like GitHub uses.
 */
export function relativeTime(dateStr: string, now?: number): string {
  const ref = now ?? Date.now();
  const then = new Date(dateStr).getTime();
  const diffMs = ref - then;
  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHr = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHr / 24);

  if (diffMin < 1) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHr < 24) return `${diffHr}h ago`;
  if (diffDay < 7) return `${diffDay}d ago`;
  return new Date(dateStr).toLocaleDateString();
}

/**
 * Normalize an API response to a comment array.
 * Guards against non-array responses (e.g. error objects, undefined).
 */
export function normalizeComments(data: unknown): unknown[] {
  return Array.isArray(data) ? data : [];
}

/**
 * Build badge text for a comment count, or return null if the badge
 * should not be shown (zero, negative, or non-numeric input).
 */
export function badgeText(count: unknown): string | null {
  if (!Number.isInteger(count) || (count as number) <= 0) return null;
  const n = count as number;
  return `${String(n)} comment${n === 1 ? "" : "s"}`;
}
