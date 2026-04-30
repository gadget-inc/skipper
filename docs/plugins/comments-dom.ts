/** @fileoverview Testable DOM functions for docs commenting. */

const HIGHLIGHT_CLASS = "docs-comment-highlight";

interface TextRange {
  node: Text;
  start: number;
  end: number;
}

interface HighlightComment {
  id: string;
  selectedText: string;
}

/**
 * Walk text nodes inside a container, building a flat text map, and return
 * the ranges (node + offsets) that cover the given search text.
 */
export function findTextIn(container: Element, text: string): TextRange[] | null {
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  while (walker.nextNode()) nodes.push(walker.currentNode as Text);

  let flat = "";
  const map: { node: Text; start: number; end: number }[] = [];
  for (const node of nodes) {
    const start = flat.length;
    flat += node.textContent;
    map.push({ node, start, end: flat.length });
  }

  const idx = flat.indexOf(text);
  if (idx === -1) return null;

  const endIdx = idx + text.length;
  const ranges: TextRange[] = [];
  for (const entry of map) {
    if (entry.end <= idx) continue;
    if (entry.start >= endIdx) break;
    const rangeStart = Math.max(0, idx - entry.start);
    const rangeEnd = Math.min(entry.node.textContent!.length, endIdx - entry.start);
    ranges.push({ node: entry.node, start: rangeStart, end: rangeEnd });
  }

  return ranges;
}

/**
 * Wrap a range in a <mark> element. Uses surroundContents when possible,
 * falls back to extractContents + insertNode for cross-element ranges
 * (e.g. headers with nested anchor tags).
 */
export function wrapRange(entry: TextRange, commentId: string): HTMLElement {
  const range = document.createRange();
  range.setStart(entry.node, entry.start);
  range.setEnd(entry.node, entry.end);

  const mark = document.createElement("mark");
  mark.className = HIGHLIGHT_CLASS;
  mark.dataset.commentId = commentId;
  mark.style.cssText =
    "background: rgba(56, 139, 253, 0.15); border-bottom: 2px solid rgba(56, 139, 253, 0.4); color: inherit; cursor: pointer; border-radius: 2px;";

  try {
    range.surroundContents(mark);
  } catch {
    mark.appendChild(range.extractContents());
    range.insertNode(mark);
  }

  return mark;
}

/**
 * Apply highlight marks for all comments within a container.
 */
export function applyHighlights(container: Element, comments: HighlightComment[]): void {
  for (const comment of comments) {
    const ranges = findTextIn(container, comment.selectedText);
    if (!ranges) continue;
    for (const entry of ranges) {
      // Skip if this text node is already inside a highlight mark
      if (entry.node.parentElement?.closest(`.${HIGHLIGHT_CLASS}`)) continue;
      wrapRange(entry, comment.id);
    }
  }
}

/**
 * Remove all highlight marks, restoring the original text nodes.
 */
export function clearHighlights(container: Element): void {
  for (const el of container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)) {
    const parent = el.parentNode!;
    while (el.firstChild) parent.insertBefore(el.firstChild, el);
    parent.removeChild(el);
    parent.normalize();
  }
}

// --- Commentable zones ---

/** CSS selectors for each commentable zone. */
const ZONE_SELECTORS: Record<string, string> = {
  content: ".sl-markdown-content",
  title: "h1#_top",
  sidebar: "#starlight__sidebar",
  toc: "starlight-toc",
};

/**
 * Return all commentable containers on the page.
 */
export function getCommentableContainers(): { zone: string; el: Element }[] {
  const result: { zone: string; el: Element }[] = [];
  for (const [zone, selector] of Object.entries(ZONE_SELECTORS)) {
    const el = document.querySelector(selector);
    if (el) result.push({ zone, el });
  }
  return result;
}

/**
 * Determine which zone a DOM node belongs to.
 */
export function detectZone(node: Node): string | null {
  const el = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement;
  if (!el) return null;
  for (const [zone, selector] of Object.entries(ZONE_SELECTORS)) {
    const container = document.querySelector(selector);
    if (container?.contains(el)) return zone;
  }
  return null;
}

/**
 * Scroll to and pulse the highlight for a comment on the current page.
 */
export function scrollToHighlight(commentId: string): void {
  const mark = document.querySelector(`.${HIGHLIGHT_CLASS}[data-comment-id="${commentId}"]`) as HTMLElement | null;
  if (!mark) return;
  mark.scrollIntoView({ block: "center", behavior: "smooth" });
  const orig = mark.style.background;
  mark.style.background = "rgba(56, 139, 253, 0.4)";
  setTimeout(() => (mark.style.background = orig), 1200);
}

export { HIGHLIGHT_CLASS };
