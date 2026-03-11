/**
 * @vitest-environment happy-dom
 */

import { describe, it, expect, vi, afterEach } from "vitest";
import {
  findTextIn,
  wrapRange,
  applyHighlights,
  clearHighlights,
  getCommentableContainers,
  detectZone,
  scrollToHighlight,
  HIGHLIGHT_CLASS,
} from "./comments-dom.ts";

/** Create a container with the given inner HTML. */
function html(content: string): HTMLDivElement {
  const el = document.createElement("div");
  el.innerHTML = content;
  return el;
}

describe("findTextIn", () => {
  it("finds text in a plain paragraph", () => {
    const container = html("<p>Hello world, this is a test.</p>");
    const ranges = findTextIn(container, "world");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].node.textContent).toBe("Hello world, this is a test.");
    expect(ranges![0].start).toBe(6);
    expect(ranges![0].end).toBe(11);
  });

  it("finds text inside a heading with an anchor", () => {
    const container = html('<h2 id="scaling"><a href="#scaling">Scaling Behavior</a></h2>');
    const ranges = findTextIn(container, "Scaling");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(7);
  });

  it("finds text spanning multiple text nodes", () => {
    const container = html("<p>Hello <strong>bold</strong> world</p>");
    const ranges = findTextIn(container, "bold world");
    expect(ranges).toHaveLength(2);
    expect(ranges![0].node.textContent).toBe("bold");
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(4);
    expect(ranges![1].node.textContent).toBe(" world");
    expect(ranges![1].start).toBe(0);
    expect(ranges![1].end).toBe(6);
  });

  it("returns null when text is not found", () => {
    const container = html("<p>Hello world</p>");
    expect(findTextIn(container, "missing")).toBeNull();
  });

  it("returns null for empty container", () => {
    const container = html("");
    expect(findTextIn(container, "anything")).toBeNull();
  });

  it("finds text at the start of a paragraph", () => {
    const container = html("<p>Start of text here.</p>");
    const ranges = findTextIn(container, "Start");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(5);
  });

  it("finds text at the end of a paragraph", () => {
    const container = html("<p>Some text here.</p>");
    const ranges = findTextIn(container, "here.");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(10);
    expect(ranges![0].end).toBe(15);
  });

  it("finds text across multiple paragraphs", () => {
    const container = html("<p>First paragraph.</p><p>Second paragraph.</p>");
    const ranges = findTextIn(container, "Second");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(6);
  });

  it("returns the first occurrence when text appears twice", () => {
    const container = html("<p>the cat and the dog</p>");
    const ranges = findTextIn(container, "the");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(3);
  });

  it("finds text spanning 3+ nodes", () => {
    const container = html("<p>A <em>B</em> <strong>C</strong> D</p>");
    const ranges = findTextIn(container, "B C");
    expect(ranges).toHaveLength(3);
    expect(ranges![0].node.textContent).toBe("B");
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(1);
    expect(ranges![1].node.textContent).toBe(" ");
    expect(ranges![1].start).toBe(0);
    expect(ranges![1].end).toBe(1);
    expect(ranges![2].node.textContent).toBe("C");
    expect(ranges![2].start).toBe(0);
    expect(ranges![2].end).toBe(1);
  });

  it("finds a single character", () => {
    const container = html("<p>Hello</p>");
    const ranges = findTextIn(container, "H");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(1);
  });

  it("finds text that matches the entire content", () => {
    const container = html("<p>Hello world</p>");
    const ranges = findTextIn(container, "Hello world");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(11);
  });

  it("matches whitespace between nodes literally", () => {
    const container = html("<p>Hello <em>world</em></p>");
    // The flat text is "Hello world" — the space is part of the first text node
    const ranges = findTextIn(container, "Hello world");
    expect(ranges).toHaveLength(2);
    expect(ranges![0].node.textContent).toBe("Hello ");
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(6);
    expect(ranges![1].node.textContent).toBe("world");
    expect(ranges![1].start).toBe(0);
    expect(ranges![1].end).toBe(5);
  });

  it("finds deeply nested text", () => {
    const container = html("<div><section><p><span>deep</span></p></section></div>");
    const ranges = findTextIn(container, "deep");
    expect(ranges).toHaveLength(1);
    expect(ranges![0].node.textContent).toBe("deep");
    expect(ranges![0].start).toBe(0);
    expect(ranges![0].end).toBe(4);
  });
});

describe("wrapRange", () => {
  it("wraps a range in a paragraph with a mark element", () => {
    const container = html("<p>Hello world</p>");
    const ranges = findTextIn(container, "world")!;
    wrapRange(ranges[0], "comment-1");

    const mark = container.querySelector(`.${HIGHLIGHT_CLASS}`);
    expect(mark).not.toBeNull();
    expect(mark!.textContent).toBe("world");
    expect((mark as HTMLElement).dataset.commentId).toBe("comment-1");
    expect(container.textContent).toBe("Hello world");
  });

  it("wraps a range inside a heading anchor without throwing", () => {
    const container = html('<h2><a href="#x">Header Text</a></h2>');
    const ranges = findTextIn(container, "Header")!;
    // Should not throw even though the range is inside a nested element
    wrapRange(ranges[0], "comment-2");

    const mark = container.querySelector(`.${HIGHLIGHT_CLASS}`);
    expect(mark).not.toBeNull();
    expect(mark!.textContent).toBe("Header");
    expect(container.textContent).toBe("Header Text");
  });

  it("returns the created mark element", () => {
    const container = html("<p>Hello world</p>");
    const ranges = findTextIn(container, "world")!;
    const mark = wrapRange(ranges[0], "comment-ret");

    expect(mark).toBeInstanceOf(HTMLElement);
    expect(mark.tagName).toBe("MARK");
    expect(mark.className).toBe(HIGHLIGHT_CLASS);
    expect(mark.dataset.commentId).toBe("comment-ret");
    expect(mark.textContent).toBe("world");
  });

  it("wraps a text node that is the only content", () => {
    const container = html("<p>entire</p>");
    const ranges = findTextIn(container, "entire")!;
    wrapRange(ranges[0], "comment-full");

    const p = container.querySelector("p")!;
    expect(p.children).toHaveLength(1);
    const mark = p.querySelector(`.${HIGHLIGHT_CLASS}`);
    expect(mark!.textContent).toBe("entire");
    expect(container.textContent).toBe("entire");
  });

  it("creates correct text node siblings for a partial wrap", () => {
    const container = html("<p>Hello world goodbye</p>");
    const ranges = findTextIn(container, "world")!;
    wrapRange(ranges[0], "comment-mid");

    const p = container.querySelector("p")!;
    // Structure should be: "Hello " <mark>world</mark> " goodbye"
    const childNodes = Array.from(p.childNodes);
    expect(childNodes).toHaveLength(3);
    expect(childNodes[0].nodeType).toBe(Node.TEXT_NODE);
    expect(childNodes[0].textContent).toBe("Hello ");
    expect((childNodes[1] as Element).tagName).toBe("MARK");
    expect(childNodes[1].textContent).toBe("world");
    expect(childNodes[2].nodeType).toBe(Node.TEXT_NODE);
    expect(childNodes[2].textContent).toBe(" goodbye");
  });
});

describe("applyHighlights", () => {
  it("highlights multiple comments", () => {
    const container = html("<p>The quick brown fox jumps over the lazy dog.</p>");
    const comments = [
      { id: "c1", selectedText: "quick brown" },
      { id: "c2", selectedText: "lazy dog" },
    ];

    applyHighlights(container, comments);

    const marks = container.querySelectorAll(`.${HIGHLIGHT_CLASS}`);
    expect(marks).toHaveLength(2);
    expect(marks[0].textContent).toBe("quick brown");
    expect((marks[0] as HTMLElement).dataset.commentId).toBe("c1");
    expect(marks[1].textContent).toBe("lazy dog");
    expect((marks[1] as HTMLElement).dataset.commentId).toBe("c2");
  });

  it("preserves full text content after highlighting", () => {
    const container = html("<p>Hello world, this is great.</p>");
    applyHighlights(container, [{ id: "c1", selectedText: "world" }]);
    expect(container.textContent).toBe("Hello world, this is great.");
  });

  it("skips comments whose text is not found", () => {
    const container = html("<p>Some content here.</p>");
    applyHighlights(container, [{ id: "c1", selectedText: "missing text" }]);
    expect(container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)).toHaveLength(0);
  });

  it("handles an empty comments array without errors", () => {
    const container = html("<p>Unchanged content.</p>");
    applyHighlights(container, []);
    expect(container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)).toHaveLength(0);
    expect(container.textContent).toBe("Unchanged content.");
  });

  it("highlights adjacent words", () => {
    const container = html("<p>quick brown fox</p>");
    applyHighlights(container, [
      { id: "c1", selectedText: "quick" },
      { id: "c2", selectedText: "brown" },
    ]);

    const marks = container.querySelectorAll(`.${HIGHLIGHT_CLASS}`);
    expect(marks).toHaveLength(2);
    expect(marks[0].textContent).toBe("quick");
    expect(marks[1].textContent).toBe("brown");
    expect(container.textContent).toBe("quick brown fox");
  });

  it("skips duplicate selected text already inside a highlight", () => {
    const container = html("<p>Hello world, this is a test.</p>");
    applyHighlights(container, [
      { id: "c1", selectedText: "Hello" },
      { id: "c2", selectedText: "Hello" },
    ]);

    const marks = container.querySelectorAll(`.${HIGHLIGHT_CLASS}`);
    // Second comment targets the same text — should not create a nested mark
    expect(marks).toHaveLength(1);
    expect((marks[0] as HTMLElement).dataset.commentId).toBe("c1");
    expect(container.textContent).toBe("Hello world, this is a test.");
  });

  it("highlights text in a heading with nested anchor", () => {
    const container = html(
      '<h2 id="scale"><a href="#scale">Scaling</a></h2><p>Some paragraph text.</p>',
    );
    applyHighlights(container, [{ id: "c1", selectedText: "Scaling" }]);

    const mark = container.querySelector(`.${HIGHLIGHT_CLASS}`);
    expect(mark).not.toBeNull();
    expect(mark!.textContent).toBe("Scaling");
  });
});

describe("clearHighlights", () => {
  it("removes highlights and restores original text", () => {
    const container = html("<p>Hello world</p>");
    applyHighlights(container, [{ id: "c1", selectedText: "world" }]);
    expect(container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)).toHaveLength(1);

    clearHighlights(container);
    expect(container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)).toHaveLength(0);
    expect(container.textContent).toBe("Hello world");
  });

  it("restores text after clearing multiple highlights", () => {
    const container = html("<p>The quick brown fox.</p>");
    applyHighlights(container, [
      { id: "c1", selectedText: "quick" },
      { id: "c2", selectedText: "fox" },
    ]);
    clearHighlights(container);
    expect(container.textContent).toBe("The quick brown fox.");
    expect(container.querySelectorAll(`.${HIGHLIGHT_CLASS}`)).toHaveLength(0);
  });

  it("is a no-op on a container with no highlights", () => {
    const container = html("<p>No highlights here.</p>");
    const before = container.innerHTML;
    clearHighlights(container);
    expect(container.innerHTML).toBe(before);
  });

  it("only removes highlight marks, not other marks", () => {
    const container = html("<p>Some <mark>custom mark</mark> text.</p>");
    // The custom mark has no HIGHLIGHT_CLASS, so clearHighlights should ignore it
    clearHighlights(container);
    const marks = container.querySelectorAll("mark");
    expect(marks).toHaveLength(1);
    expect(marks[0].textContent).toBe("custom mark");
    expect(container.textContent).toBe("Some custom mark text.");
  });
});

describe("highlight round-trip", () => {
  it("apply -> clear -> re-apply produces identical result", () => {
    const container = html("<p>Testing the round trip behavior.</p>");
    const comments = [{ id: "c1", selectedText: "round trip" }];

    applyHighlights(container, comments);
    const firstHTML = container.innerHTML;

    clearHighlights(container);
    expect(container.textContent).toBe("Testing the round trip behavior.");

    applyHighlights(container, comments);
    expect(container.innerHTML).toBe(firstHTML);
  });

  it("apply -> clear -> re-apply with heading anchors", () => {
    const container = html('<h2 id="config"><a href="#config">Configuration Options</a></h2>');
    const comments = [{ id: "c1", selectedText: "Configuration" }];

    applyHighlights(container, comments);
    clearHighlights(container);
    expect(container.textContent).toBe("Configuration Options");

    applyHighlights(container, comments);
    const mark = container.querySelector(`.${HIGHLIGHT_CLASS}`);
    expect(mark).not.toBeNull();
    expect(mark!.textContent).toBe("Configuration");
  });

  it("apply -> clear -> re-apply with multiple paragraphs", () => {
    const container = html("<p>First paragraph with some text.</p><p>Second paragraph here.</p>");
    const comments = [
      { id: "c1", selectedText: "some text" },
      { id: "c2", selectedText: "Second" },
    ];

    applyHighlights(container, comments);
    clearHighlights(container);
    applyHighlights(container, comments);

    const marks = container.querySelectorAll(`.${HIGHLIGHT_CLASS}`);
    expect(marks).toHaveLength(2);
    expect(marks[0].textContent).toBe("some text");
    expect(marks[1].textContent).toBe("Second");
  });
});

// --- Zone and scroll tests ---

/**
 * Set up a page-like DOM with zone containers.
 * Returns a cleanup function to restore body.
 */
function setupZones(
  zones: Partial<Record<"content" | "title" | "sidebar" | "toc", string | false>> = {},
): () => void {
  const els: Element[] = [];
  if (zones.content !== false) {
    const content = document.createElement("div");
    content.className = "sl-markdown-content";
    content.innerHTML = zones.content || "<p>Content text</p>";
    document.body.appendChild(content);
    els.push(content);
  }
  if (zones.title !== false) {
    const title = document.createElement("h1");
    title.id = "_top";
    title.textContent = (zones.title as string) || "Page Title";
    document.body.appendChild(title);
    els.push(title);
  }
  if (zones.sidebar !== false) {
    const sidebar = document.createElement("div");
    sidebar.id = "starlight__sidebar";
    sidebar.innerHTML = (zones.sidebar as string) || "<a>Nav Item</a>";
    document.body.appendChild(sidebar);
    els.push(sidebar);
  }
  if (zones.toc !== false) {
    const toc = document.createElement("starlight-toc");
    toc.innerHTML = (zones.toc as string) || "<a>Heading Link</a>";
    document.body.appendChild(toc);
    els.push(toc);
  }
  return () => els.forEach((el) => el.remove());
}

describe("getCommentableContainers", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("returns empty array when no zones exist", () => {
    expect(getCommentableContainers()).toEqual([]);
  });

  it("returns only zones that exist in the DOM", () => {
    const cleanup = setupZones({ title: "Test", sidebar: false, toc: false });
    const containers = getCommentableContainers();
    expect(containers).toHaveLength(2); // content + title
    expect(containers.map((c) => c.zone)).toContain("content");
    expect(containers.map((c) => c.zone)).toContain("title");
    cleanup();
  });

  it("returns all four zones when all exist", () => {
    const cleanup = setupZones({});
    const containers = getCommentableContainers();
    expect(containers).toHaveLength(4);
    expect(containers.map((c) => c.zone).sort()).toEqual(["content", "sidebar", "title", "toc"]);
    cleanup();
  });

  it("returns the actual DOM elements", () => {
    const cleanup = setupZones({});
    const containers = getCommentableContainers();
    const contentEntry = containers.find((c) => c.zone === "content")!;
    expect(contentEntry.el.className).toBe("sl-markdown-content");
    cleanup();
  });
});

describe("detectZone", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("detects an element inside the content zone", () => {
    const cleanup = setupZones({});
    const p = document.querySelector(".sl-markdown-content p")!;
    expect(detectZone(p)).toBe("content");
    cleanup();
  });

  it("detects a text node via its parent element", () => {
    const cleanup = setupZones({});
    const textNode = document.querySelector("h1#_top")!.firstChild!;
    expect(textNode.nodeType).toBe(Node.TEXT_NODE);
    expect(detectZone(textNode)).toBe("title");
    cleanup();
  });

  it("returns null for a node outside all zones", () => {
    const cleanup = setupZones({});
    const orphan = document.createElement("span");
    orphan.textContent = "orphan";
    document.body.appendChild(orphan);
    expect(detectZone(orphan)).toBeNull();
    orphan.remove();
    cleanup();
  });

  it("returns null for a text node with no parent", () => {
    const textNode = document.createTextNode("detached");
    expect(detectZone(textNode)).toBeNull();
  });

  it("detects the zone container element itself", () => {
    const cleanup = setupZones({});
    const sidebar = document.querySelector("#starlight__sidebar")!;
    expect(detectZone(sidebar)).toBe("sidebar");
    cleanup();
  });
});

describe("scrollToHighlight", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("pulses the background and restores it after timeout", () => {
    vi.useFakeTimers();

    const container = html("<p>Hello world</p>");
    document.body.appendChild(container);
    applyHighlights(container, [{ id: "c1", selectedText: "world" }]);

    const mark = document.querySelector(`.${HIGHLIGHT_CLASS}`) as HTMLElement;
    const origBg = mark.style.background;

    scrollToHighlight("c1");

    expect(mark.style.background).toBe("rgba(56, 139, 253, 0.4)");

    vi.advanceTimersByTime(1200);
    expect(mark.style.background).toBe(origBg);

    vi.useRealTimers();
  });

  it("is a no-op when the comment ID does not exist", () => {
    // Should not throw
    scrollToHighlight("nonexistent");
  });

  it("calls scrollIntoView on the mark element", () => {
    const container = html("<p>Hello world</p>");
    document.body.appendChild(container);
    applyHighlights(container, [{ id: "c1", selectedText: "world" }]);

    const mark = document.querySelector(`.${HIGHLIGHT_CLASS}`) as HTMLElement;
    const spy = vi.spyOn(mark, "scrollIntoView");

    scrollToHighlight("c1");

    expect(spy).toHaveBeenCalledWith({ block: "center", behavior: "smooth" });
  });
});
