/** @fileoverview Client-side commenting overlay for docs review. Dev-only. */

import {
  applyHighlights,
  clearHighlights,
  getCommentableContainers,
  detectZone,
  scrollToHighlight,
  HIGHLIGHT_CLASS,
} from "./comments-dom.ts";
import { normalizeComments, badgeText, relativeTime } from "./comments-utils.ts";

interface Comment {
  id: string;
  page: string;
  zone?: string;
  selectedText: string;
  contextBefore: string;
  contextAfter: string;
  comment: string;
  createdAt: string;
}

const CONTAINER_ID = "docs-comments-container";

// --- State ---

let comments: Comment[] = [];

// --- Helpers ---

/** Return Primer color tokens matching the current Starlight theme. */
function themeColors(): Record<string, string> {
  const dark = document.documentElement.dataset.theme === "dark";
  return dark
    ? {
        bgDefault: "#0d1117",
        bgMuted: "#161b22",
        bgOverlay: "#161b22",
        bgInset: "#010409",
        fgDefault: "#e6edf3",
        fgMuted: "#9198a1",
        borderDefault: "#3d444d",
        accentFg: "#4493f8",
        accentEmphasis: "#388bfd",
        accentMuted: "rgba(56,139,253,0.15)",
        dangerFg: "#f85149",
        dangerMuted: "rgba(248,81,73,0.1)",
        dangerBorder: "rgba(248,81,73,0.4)",
        successEmphasis: "#238636",
        btnBg: "#21262d",
        shadow: "rgba(0,0,0,0.4)",
        shadowHeavy: "rgba(0,0,0,0.5)",
      }
    : {
        bgDefault: "#ffffff",
        bgMuted: "#f6f8fa",
        bgOverlay: "#ffffff",
        bgInset: "#f6f8fa",
        fgDefault: "#1f2328",
        fgMuted: "#59636e",
        borderDefault: "#d1d9e0",
        accentFg: "#0969da",
        accentEmphasis: "#0969da",
        accentMuted: "rgba(9,105,218,0.15)",
        dangerFg: "#d1242f",
        dangerMuted: "rgba(209,36,47,0.08)",
        dangerBorder: "rgba(209,36,47,0.4)",
        successEmphasis: "#1a7f37",
        btnBg: "#f3f4f6",
        shadow: "rgba(31,35,40,0.12)",
        shadowHeavy: "rgba(31,35,40,0.2)",
      };
}

// --- API ---

const page = window.location.pathname;

async function fetchComments(): Promise<void> {
  const res = await fetch(`/api/comments?page=${encodeURIComponent(page)}`);
  comments = normalizeComments(await res.json()) as Comment[];
}

async function createComment(data: Omit<Comment, "id" | "page" | "createdAt">): Promise<Comment> {
  const res = await fetch("/api/comments", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ ...data, page }),
  });
  if (!res.ok) throw new Error(`Failed to create comment: ${res.status}`);
  const comment = (await res.json()) as Comment;
  comments.push(comment);
  return comment;
}

async function deleteComment(id: string): Promise<void> {
  const res = await fetch(`/api/comments?id=${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) throw new Error(`Failed to delete comment: ${res.status}`);
  comments = comments.filter((c) => c.id !== id);
}

async function updateComment(id: string, newText: string): Promise<Comment> {
  const res = await fetch(`/api/comments?id=${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ comment: newText }),
  });
  if (!res.ok) throw new Error(`Failed to update comment: ${res.status}`);
  const updated = (await res.json()) as Comment;
  comments = comments.map((c) => (c.id === id ? updated : c));
  return updated;
}

async function fetchAllComments(): Promise<Comment[]> {
  const res = await fetch("/api/comments");
  const data: unknown = await res.json();
  return normalizeComments(data) as Comment[];
}

// --- Highlighting ---

function renderHighlights(): void {
  const containers = getCommentableContainers();
  for (const { zone, el } of containers) {
    clearHighlights(el);
    const zoneComments = comments.filter((c) => (c.zone || "content") === zone);
    applyHighlights(el, zoneComments);
  }
}

// --- UI: Comment button on selection ---

let selectionButton: HTMLButtonElement | null = null;

function removeSelectionButton(): void {
  selectionButton?.remove();
  selectionButton = null;
}

function showSelectionButton(zone: string): void {
  removeSelectionButton();
  const t = themeColors();

  const sel = window.getSelection();
  if (!sel || sel.rangeCount === 0) return;
  const selRect = sel.getRangeAt(0).getBoundingClientRect();

  const btn = document.createElement("button");
  btn.textContent = "+";
  btn.style.cssText = `
    position: fixed; left: ${selRect.left}px; top: ${selRect.bottom + 8}px; z-index: 10000;
    background: ${t.accentFg}; color: #ffffff; border: none; border-radius: 5px;
    width: 24px; height: 24px; padding: 0 0 1px; font-size: 16px; line-height: 24px;
    font-family: system-ui; cursor: pointer; display: flex; align-items: center;
    justify-content: center; box-shadow: 0 1px 4px ${t.shadow};
  `;
  btn.addEventListener("mousedown", (e) => {
    e.preventDefault();
    e.stopPropagation();
    openCommentForm(zone);
  });

  document.body.appendChild(btn);
  selectionButton = btn;
}

// --- UI: Comment form ---

function openCommentForm(zone: string): void {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed) return;

  const selectedText = sel.toString().trim();
  if (!selectedText) return;

  // Grab surrounding context for disambiguation
  const range = sel.getRangeAt(0);
  const container = range.commonAncestorContainer;
  const fullText =
    container.nodeType === Node.TEXT_NODE
      ? (container.parentElement?.textContent ?? container.textContent ?? "")
      : (container.textContent ?? "");

  const pos = fullText.indexOf(selectedText);
  const contextBefore = pos > 0 ? fullText.slice(Math.max(0, pos - 60), pos) : "";
  const contextAfter =
    pos >= 0 ? fullText.slice(pos + selectedText.length, pos + selectedText.length + 60) : "";

  removeSelectionButton();

  const rect = range.getBoundingClientRect();
  const t = themeColors();

  const overlay = document.createElement("div");
  overlay.id = CONTAINER_ID;
  overlay.style.cssText = `
    position: fixed; left: ${Math.min(rect.left, window.innerWidth - 480)}px;
    top: ${rect.bottom + 8}px; z-index: 10001;
    background: ${t.bgDefault}; border: 1px solid ${t.borderDefault}; border-radius: 6px;
    width: 460px; font-family: system-ui; font-size: 13px;
    box-shadow: 0 8px 24px ${t.shadow}; color: ${t.fgDefault};
    overflow: hidden;
  `;

  // Header bar
  const header = document.createElement("div");
  header.textContent = "Add a review comment";
  header.style.cssText = `
    padding: 8px 12px; background: ${t.bgMuted}; border-bottom: 1px solid ${t.borderDefault};
    font-size: 12px; font-weight: 600; color: ${t.fgDefault};
  `;
  overlay.appendChild(header);

  // Selected text preview
  const preview = document.createElement("div");
  preview.style.cssText = `
    border-left: 3px solid ${t.borderDefault}; background: ${t.bgMuted}; padding: 8px 12px;
    margin: 12px; font-size: 12px; line-height: 1.4; max-height: 60px;
    overflow: hidden; color: ${t.fgMuted}; border-radius: 3px;
  `;
  preview.textContent =
    selectedText.length > 120 ? selectedText.slice(0, 120) + "\u2026" : selectedText;
  overlay.appendChild(preview);

  // Textarea wrapper with "Write" tab
  const textareaWrapper = document.createElement("div");
  textareaWrapper.style.cssText = `
    margin: 0 12px; border: 1px solid ${t.borderDefault}; border-radius: 6px; overflow: hidden;
  `;

  const writeTab = document.createElement("div");
  writeTab.textContent = "Write";
  writeTab.style.cssText = `
    padding: 6px 12px; background: ${t.bgDefault}; border-bottom: 1px solid ${t.borderDefault};
    font-size: 12px; color: ${t.fgDefault}; font-weight: 500;
  `;
  textareaWrapper.appendChild(writeTab);

  const textarea = document.createElement("textarea");
  textarea.placeholder = "Add your comment\u2026";
  textarea.style.cssText = `
    width: 100%; height: 70px; resize: vertical;
    background: ${t.bgDefault}; border: none;
    padding: 8px 12px; color: ${t.fgDefault}; font-family: system-ui;
    font-size: 13px; outline: none; box-sizing: border-box;
  `;
  textarea.addEventListener("focus", () => (textareaWrapper.style.borderColor = t.accentEmphasis));
  textarea.addEventListener("blur", () => (textareaWrapper.style.borderColor = t.borderDefault));
  textarea.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      save.click();
    }
  });
  textareaWrapper.appendChild(textarea);
  overlay.appendChild(textareaWrapper);

  // Actions bar
  const actions = document.createElement("div");
  actions.style.cssText = "padding: 8px 12px; display: flex; gap: 8px; justify-content: flex-end;";

  const cancel = document.createElement("button");
  cancel.textContent = "Cancel";
  cancel.style.cssText = `
    padding: 5px 16px; border: 1px solid ${t.borderDefault}; border-radius: 6px;
    background: ${t.btnBg}; color: ${t.fgDefault}; cursor: pointer; font-size: 12px; font-weight: 500;
  `;
  cancel.addEventListener("click", () => overlay.remove());

  const save = document.createElement("button");
  save.textContent = "Comment \u2318\u21b5";
  save.style.cssText = `
    padding: 5px 16px; border: none; border-radius: 6px;
    background: ${t.successEmphasis}; color: #ffffff; cursor: pointer; font-size: 12px; font-weight: 500;
  `;
  save.addEventListener("click", async () => {
    const text = textarea.value.trim();
    if (!text) return;

    save.disabled = true;
    save.textContent = "Saving\u2026";

    try {
      const created = await createComment({
        selectedText,
        contextBefore,
        contextAfter,
        comment: text,
        zone: zone || "content",
      });

      overlay.remove();
      renderHighlights();
      const all = await fetchAllComments();
      renderBadge(all.length);
      await showAllCommentsPanel(created.id);
    } catch (err) {
      console.error("Failed to save comment:", err);
      save.disabled = false;
      save.textContent = "Save";
    }
  });

  actions.appendChild(cancel);
  actions.appendChild(save);
  overlay.appendChild(actions);

  document.body.appendChild(overlay);
  textarea.focus();
}

// --- UI: Comment popover on highlight click ---

function showCommentPopover(commentId: string, target: Element): void {
  // Remove any existing popover
  document.getElementById("docs-comment-popover")?.remove();

  const comment = comments.find((c) => c.id === commentId);
  if (!comment) return;

  const rect = target.getBoundingClientRect();
  const t = themeColors();

  const popover = document.createElement("div");
  popover.id = "docs-comment-popover";
  popover.dataset.commentId = commentId;
  popover.style.cssText = `
    position: fixed; left: ${Math.min(rect.left, window.innerWidth - 300)}px;
    top: ${rect.bottom + 6}px; z-index: 10001;
    background: ${t.bgDefault}; border: 1px solid ${t.borderDefault}; border-radius: 6px;
    width: 300px; font-family: system-ui; font-size: 13px;
    box-shadow: 0 8px 24px ${t.shadow}; overflow: hidden;
  `;

  // Header bar with timestamp
  const header = document.createElement("div");
  header.style.cssText = `
    display: flex; align-items: center; justify-content: space-between;
    padding: 8px 12px; background: ${t.bgMuted}; border-bottom: 1px solid ${t.borderDefault};
    font-size: 12px; color: ${t.fgMuted};
  `;
  header.textContent = relativeTime(comment.createdAt);
  popover.appendChild(header);

  // Comment body
  const text = document.createElement("div");
  text.textContent = comment.comment;
  text.style.cssText = `padding: 12px; color: ${t.fgDefault}; line-height: 1.5;`;
  popover.appendChild(text);

  // Actions
  const actions = document.createElement("div");
  actions.style.cssText = `
    padding: 8px 12px; border-top: 1px solid ${t.borderDefault};
    display: flex; gap: 8px; justify-content: flex-end;
  `;

  const del = document.createElement("button");
  del.textContent = "Delete";
  del.style.cssText = `
    padding: 3px 10px; border: 1px solid ${t.dangerBorder}; border-radius: 6px;
    background: ${t.dangerMuted}; color: ${t.dangerFg}; cursor: pointer; font-size: 12px;
  `;
  del.addEventListener("click", async () => {
    try {
      await deleteComment(commentId);
    } catch (err) {
      console.error("Failed to save comment:", err);
      return;
    }
    popover.remove();
    renderHighlights();
    void fetchAllComments().then((all) => renderBadge(all.length));
  });

  const close = document.createElement("button");
  close.textContent = "Dismiss";
  close.style.cssText = `
    padding: 3px 10px; border: 1px solid ${t.borderDefault}; border-radius: 6px;
    background: ${t.btnBg}; color: ${t.fgDefault}; cursor: pointer; font-size: 12px;
  `;
  close.addEventListener("click", () => popover.remove());

  const edit = document.createElement("button");
  edit.textContent = "Edit";
  edit.style.cssText = `
    padding: 3px 10px; border: 1px solid ${t.borderDefault}; border-radius: 6px;
    background: ${t.btnBg}; color: ${t.accentFg}; cursor: pointer; font-size: 12px;
  `;
  edit.addEventListener("click", () => {
    // Replace text with textarea
    const textarea = document.createElement("textarea");
    textarea.value = comment.comment;
    textarea.style.cssText = `
      width: 100%; height: 70px; resize: vertical;
      background: ${t.bgDefault}; border: 1px solid ${t.borderDefault};
      padding: 8px; color: ${t.fgDefault}; font-family: system-ui;
      font-size: 13px; outline: none; box-sizing: border-box; border-radius: 4px;
    `;
    textarea.addEventListener("focus", () => (textarea.style.borderColor = t.accentEmphasis));
    textarea.addEventListener("blur", () => (textarea.style.borderColor = t.borderDefault));

    text.replaceWith(textarea);
    textarea.focus();

    // Replace actions with Save/Cancel
    const editActions = document.createElement("div");
    editActions.style.cssText = actions.style.cssText;

    const cancelEdit = document.createElement("button");
    cancelEdit.textContent = "Cancel";
    cancelEdit.style.cssText = close.style.cssText;
    cancelEdit.addEventListener("click", () => {
      textarea.replaceWith(text);
      editActions.replaceWith(actions);
    });

    const saveEdit = document.createElement("button");
    saveEdit.textContent = "Save";
    saveEdit.style.cssText = `
      padding: 3px 10px; border: none; border-radius: 6px;
      background: ${t.successEmphasis}; color: #ffffff; cursor: pointer; font-size: 12px;
    `;
    saveEdit.addEventListener("click", async () => {
      const newText = textarea.value.trim();
      if (!newText) return;
      saveEdit.disabled = true;
      saveEdit.textContent = "Saving\u2026";
      try {
        await updateComment(commentId, newText);
        popover.remove();
        showCommentPopover(commentId, target);
      } catch (err) {
        console.error("Failed to save comment:", err);
        saveEdit.disabled = false;
        saveEdit.textContent = "Save";
      }
    });

    textarea.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        saveEdit.click();
      }
    });

    editActions.appendChild(cancelEdit);
    editActions.appendChild(saveEdit);
    actions.replaceWith(editActions);
  });

  actions.appendChild(del);
  actions.appendChild(edit);
  actions.appendChild(close);
  popover.appendChild(actions);

  document.body.appendChild(popover);
}

// --- UI: All Comments panel ---

const ALL_COMMENTS_PANEL_ID = "docs-all-comments-panel";

async function showAllCommentsPanel(focusId?: string): Promise<void> {
  // Remove existing panel
  document.getElementById(ALL_COMMENTS_PANEL_ID)?.remove();

  const allComments = await fetchAllComments();
  const t = themeColors();

  const panel = document.createElement("div");
  panel.id = ALL_COMMENTS_PANEL_ID;
  panel.style.cssText = `
    position: fixed; top: 0; right: 0; bottom: 0; width: 400px; z-index: 10002;
    background: ${t.bgDefault}; border-left: 1px solid ${t.borderDefault}; color: ${t.fgDefault};
    font-family: system-ui; font-size: 13px; display: flex; flex-direction: column;
    box-shadow: -4px 0 20px ${t.shadowHeavy};
  `;

  // Header
  const header = document.createElement("div");
  header.style.cssText = `
    display: flex; align-items: center; justify-content: space-between;
    padding: 16px; border-bottom: 1px solid ${t.borderDefault}; flex-shrink: 0;
    background: ${t.bgMuted};
  `;
  const title = document.createElement("span");
  title.textContent = "All Comments";
  title.style.cssText = `font-size: 14px; font-weight: 600; color: ${t.fgDefault};`;
  const closeBtn = document.createElement("button");
  closeBtn.textContent = "\u00d7";
  closeBtn.style.cssText = `
    background: none; border: none; color: ${t.fgMuted}; font-size: 22px;
    cursor: pointer; padding: 0 4px; line-height: 1;
  `;
  closeBtn.addEventListener("click", () => panel.remove());
  header.appendChild(title);
  header.appendChild(closeBtn);
  panel.appendChild(header);

  // Content area
  const content = document.createElement("div");
  content.style.cssText = "flex: 1; overflow-y: auto; padding: 12px 16px;";

  if (allComments.length === 0) {
    const empty = document.createElement("div");
    empty.textContent = "No comments yet";
    empty.style.cssText = `
      text-align: center; color: ${t.fgMuted}; padding: 40px 0; font-size: 14px;
    `;
    content.appendChild(empty);
  } else {
    // Group by page
    const byPage: Record<string, Comment[]> = {};
    for (const c of allComments) {
      if (!byPage[c.page]) byPage[c.page] = [];
      byPage[c.page].push(c);
    }

    for (const [pagePath, pageComments] of Object.entries(byPage)) {
      // Group container with border and border-radius
      const group = document.createElement("div");
      group.style.cssText = `
        border: 1px solid ${t.borderDefault}; border-radius: 6px; overflow: hidden;
        margin-bottom: 16px;
      `;

      // Page header
      const pageHeader = document.createElement("a");
      pageHeader.href = `${pagePath}#comment:${pageComments[0].id}`;
      pageHeader.textContent = pagePath;
      pageHeader.style.cssText = `
        display: block; padding: 8px 12px; background: ${t.bgMuted};
        border-bottom: 1px solid ${t.borderDefault}; font-size: 12px; font-weight: 600;
        color: ${t.fgDefault}; text-decoration: none;
      `;
      pageHeader.addEventListener("click", (e) => {
        if (pagePath === page) {
          e.preventDefault();
          scrollToHighlight(pageComments[0].id);
        } else {
          panel.remove();
        }
      });
      group.appendChild(pageHeader);

      for (let i = 0; i < pageComments.length; i++) {
        const c = pageComments[i];
        const isLast = i === pageComments.length - 1;

        const card = document.createElement("div");
        card.dataset.commentCard = c.id;
        const isFocused = c.id === focusId;
        card.style.cssText = `
          padding: 12px; cursor: pointer;${isLast ? "" : ` border-bottom: 1px solid ${t.borderDefault};`}${isFocused ? ` background: ${t.accentMuted};` : ""}
        `;

        card.addEventListener("click", (e) => {
          if ((e.target as Element).closest("button")) return;
          if (pagePath === page) {
            scrollToHighlight(c.id);
          } else {
            window.location.href = `${pagePath}#comment:${c.id}`;
            panel.remove();
          }
        });

        // Selected text preview
        const preview = document.createElement("div");
        preview.style.cssText = `
          border-left: 3px solid ${t.borderDefault}; background: ${t.bgMuted}; padding: 6px 10px;
          margin-bottom: 8px; font-size: 12px; color: ${t.fgMuted}; border-radius: 3px;
          max-height: 40px; overflow: hidden; line-height: 1.4;
        `;
        preview.textContent =
          c.selectedText.length > 100 ? c.selectedText.slice(0, 100) + "\u2026" : c.selectedText;
        card.appendChild(preview);

        // Comment text
        const commentText = document.createElement("div");
        commentText.textContent = c.comment;
        commentText.style.cssText = `color: ${t.fgDefault}; line-height: 1.5; margin-bottom: 6px;`;
        card.appendChild(commentText);

        // Footer: timestamp + delete
        const footer = document.createElement("div");
        footer.style.cssText = `
          display: flex; align-items: center; justify-content: space-between;
        `;

        const timestamp = document.createElement("span");
        timestamp.textContent = relativeTime(c.createdAt);
        timestamp.style.cssText = `font-size: 11px; color: ${t.fgMuted};`;

        const delBtn = document.createElement("button");
        delBtn.textContent = "Delete";
        delBtn.style.cssText = `
          padding: 3px 10px; border: 1px solid ${t.dangerBorder}; border-radius: 6px;
          background: ${t.dangerMuted}; color: ${t.dangerFg};
          cursor: pointer; font-size: 12px;
        `;
        delBtn.addEventListener("click", async () => {
          await deleteComment(c.id);
          renderHighlights();
          // Refresh the panel contents
          const refreshed = await fetchAllComments();
          renderBadge(refreshed.length);
          // Re-render panel
          panel.remove();
          await showAllCommentsPanel();
        });

        const footerRight = document.createElement("div");
        footerRight.style.cssText = "display: flex; gap: 6px;";

        const editBtn = document.createElement("button");
        editBtn.textContent = "Edit";
        editBtn.style.cssText = `
          padding: 3px 10px; border: 1px solid ${t.borderDefault}; border-radius: 6px;
          background: ${t.btnBg}; color: ${t.accentFg};
          cursor: pointer; font-size: 12px;
        `;
        editBtn.addEventListener("click", async (e) => {
          e.stopPropagation();
          // Replace comment text with textarea
          const textarea = document.createElement("textarea");
          textarea.value = c.comment;
          textarea.style.cssText = `
            width: 100%; height: 60px; resize: vertical;
            background: ${t.bgDefault}; border: 1px solid ${t.borderDefault};
            padding: 8px; color: ${t.fgDefault}; font-family: system-ui;
            font-size: 13px; outline: none; box-sizing: border-box; border-radius: 4px;
            margin-bottom: 6px;
          `;
          textarea.addEventListener("focus", () => (textarea.style.borderColor = t.accentEmphasis));
          textarea.addEventListener("blur", () => (textarea.style.borderColor = t.borderDefault));
          commentText.replaceWith(textarea);
          textarea.focus();

          // Replace footer with save/cancel
          const editFooter = document.createElement("div");
          editFooter.style.cssText = `
            display: flex; gap: 8px; justify-content: flex-end;
          `;

          const cancelBtn = document.createElement("button");
          cancelBtn.textContent = "Cancel";
          cancelBtn.style.cssText = `
            padding: 3px 10px; border: 1px solid ${t.borderDefault}; border-radius: 6px;
            background: ${t.btnBg}; color: ${t.fgDefault}; cursor: pointer; font-size: 12px;
          `;
          cancelBtn.addEventListener("click", (ev) => {
            ev.stopPropagation();
            textarea.replaceWith(commentText);
            editFooter.replaceWith(footer);
          });

          const saveBtn = document.createElement("button");
          saveBtn.textContent = "Save";
          saveBtn.style.cssText = `
            padding: 3px 10px; border: none; border-radius: 6px;
            background: ${t.successEmphasis}; color: #ffffff; cursor: pointer; font-size: 12px;
          `;
          saveBtn.addEventListener("click", async (ev) => {
            ev.stopPropagation();
            const newText = textarea.value.trim();
            if (!newText) return;
            saveBtn.disabled = true;
            saveBtn.textContent = "Saving\u2026";
            await updateComment(c.id, newText);
            renderHighlights();
            panel.remove();
            await showAllCommentsPanel(c.id);
          });

          textarea.addEventListener("keydown", (ev) => {
            if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
              ev.preventDefault();
              saveBtn.click();
            }
          });

          editFooter.appendChild(cancelBtn);
          editFooter.appendChild(saveBtn);
          footer.replaceWith(editFooter);
        });

        footerRight.appendChild(editBtn);
        footerRight.appendChild(delBtn);

        footer.appendChild(timestamp);
        footer.appendChild(footerRight);
        card.appendChild(footer);

        group.appendChild(card);
      }

      content.appendChild(group);
    }
  }

  panel.appendChild(content);
  document.body.appendChild(panel);

  // Scroll to and briefly highlight the focused comment
  if (focusId) {
    const target = panel.querySelector(`[data-comment-card="${focusId}"]`) as HTMLElement | null;
    if (target) {
      target.scrollIntoView({ block: "center", behavior: "smooth" });
      // Fade out the highlight after a moment
      setTimeout(() => (target.style.background = ""), 1500);
    }
  }
}

// --- UI: Comment count badge ---

function renderBadge(totalCount: number): void {
  document.getElementById("docs-comment-badge")?.remove();

  const text = badgeText(totalCount);
  if (!text) return;

  const t = themeColors();

  const badge = document.createElement("div");
  badge.id = "docs-comment-badge";
  badge.style.cssText = `
    position: fixed; bottom: 16px; right: 16px; z-index: 9999;
    background: ${t.bgMuted}; color: ${t.fgDefault}; border: 1px solid ${t.borderDefault};
    border-radius: 6px; padding: 4px 10px; font-family: system-ui; font-size: 12px;
    font-weight: 500; box-shadow: 0 1px 4px ${t.shadow}; cursor: pointer;
    user-select: none;
  `;
  badge.textContent = text;
  badge.addEventListener("click", () => showAllCommentsPanel());

  document.body.appendChild(badge);
}

// --- Event listeners ---

document.addEventListener("mouseup", (e) => {
  // Ignore clicks inside our own UI
  if (
    (e.target as Element).closest(
      `#${CONTAINER_ID}, #docs-comment-popover, #${ALL_COMMENTS_PANEL_ID}, .${HIGHLIGHT_CLASS}`,
    )
  )
    return;

  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.toString().trim()) {
    removeSelectionButton();
    return;
  }

  const zone = detectZone(sel.anchorNode!);
  if (!zone) {
    removeSelectionButton();
    return;
  }

  showSelectionButton(zone);
});

document.addEventListener("selectionchange", () => {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.toString().trim()) {
    removeSelectionButton();
  }
});

document.addEventListener("mousedown", (e) => {
  // Close popover on outside click (skip highlights — handled in click toggle)
  if (
    !(e.target as Element).closest("#docs-comment-popover") &&
    !(e.target as Element).closest(`.${HIGHLIGHT_CLASS}`)
  ) {
    document.getElementById("docs-comment-popover")?.remove();
  }

  // Close comment form on outside click
  if (
    !(e.target as Element).closest(`#${CONTAINER_ID}`) &&
    !selectionButton?.contains(e.target as Node)
  ) {
    document.getElementById(CONTAINER_ID)?.remove();
  }

  // Close all-comments panel on outside click
  const panel = document.getElementById(ALL_COMMENTS_PANEL_ID);
  if (
    panel &&
    !panel.contains(e.target as Node) &&
    !(e.target as Element).closest("#docs-comment-badge")
  ) {
    panel.remove();
  }
});

document.addEventListener("click", (e) => {
  const highlight = (e.target as Element).closest(`.${HIGHLIGHT_CLASS}`) as HTMLElement | null;
  if (highlight) {
    e.preventDefault();
    const commentId = highlight.dataset.commentId!;
    const existing = document.getElementById("docs-comment-popover") as HTMLElement | null;
    if (existing && existing.dataset.commentId === commentId) {
      existing.remove();
    } else {
      showCommentPopover(commentId, highlight);
    }
  }
});

// Handle Escape key
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    document.getElementById(ALL_COMMENTS_PANEL_ID)?.remove();
    document.getElementById("docs-comment-popover")?.remove();
    document.getElementById(CONTAINER_ID)?.remove();
    removeSelectionButton();
  }
});

// --- Init ---

async function init(): Promise<void> {
  const [, allComments] = await Promise.all([fetchComments(), fetchAllComments()]);
  renderHighlights();
  renderBadge(allComments.length);

  // Handle #comment:<id> deep links from cross-page navigation
  const hash = window.location.hash;
  if (hash.startsWith("#comment:")) {
    const commentId = hash.slice("#comment:".length);
    history.replaceState(null, "", page); // clean up the hash
    await showAllCommentsPanel(commentId);
    // Allow highlights to render before scrolling
    requestAnimationFrame(() => scrollToHighlight(commentId));
  }
}

void init();
