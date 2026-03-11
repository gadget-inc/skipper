import { describe, it, expect, vi, beforeEach } from "vitest";
import { EventEmitter } from "node:events";
import type { ViteDevServer } from "vite";

// ---------------------------------------------------------------------------
// Hoist mock functions so they are defined before vi.mock() executes
// ---------------------------------------------------------------------------

const mocks = vi.hoisted(() => ({
  readdir: vi.fn(),
  readFile: vi.fn(),
  writeFile: vi.fn(),
  mkdir: vi.fn(),
  unlink: vi.fn(),
}));

vi.mock("node:fs/promises", () => ({
  readdir: mocks.readdir,
  readFile: mocks.readFile,
  writeFile: mocks.writeFile,
  mkdir: mocks.mkdir,
  unlink: mocks.unlink,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Capture the middleware registered via server.middlewares.use() */
function extractMiddleware(plugin: {
  configureServer?: (server: ViteDevServer) => void;
}): (req: unknown, res: unknown, next: () => void) => void {
  let captured: ((req: unknown, res: unknown, next: () => void) => void) | undefined;
  const mockServer = {
    middlewares: {
      use(fn: (req: unknown, res: unknown, next: () => void) => void) {
        captured = fn;
      },
    },
  } as unknown as ViteDevServer;

  if (typeof plugin.configureServer === "function") {
    plugin.configureServer(mockServer);
  }

  if (!captured) throw new Error("middleware was not registered");
  return captured;
}

/** Build a minimal mock request. */
function makeReq(method: string, url: string): EventEmitter & { method: string; url: string } {
  const req = new EventEmitter() as EventEmitter & { method: string; url: string };
  req.method = method;
  req.url = url;
  return req;
}

interface MockRes {
  statusCode: number;
  headers: Record<string, string>;
  body: string;
  writeHead: (status: number, headers?: Record<string, string>) => void;
  end: (data?: string) => void;
}

/** Build a minimal mock response that captures status + body. */
function makeRes(): MockRes {
  const res: MockRes = {
    statusCode: 0,
    headers: {},
    body: "",
    writeHead(status, headers = {}) {
      this.statusCode = status;
      Object.assign(this.headers, headers);
    },
    end(data = "") {
      this.body = data;
    },
  };
  return res;
}

/** Emit data/end to simulate a request body. */
function sendBody(req: EventEmitter, json: unknown): void {
  const chunk = Buffer.from(JSON.stringify(json));
  req.emit("data", chunk);
  req.emit("end");
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("comments plugin middleware", () => {
  let middleware: (req: unknown, res: unknown, next: () => void) => void;

  beforeEach(async () => {
    vi.resetAllMocks();
    // Default: mkdir always succeeds (ensureDir)
    mocks.mkdir.mockResolvedValue(undefined);

    // Dynamic import after mocks are set up; clear module cache so each
    // beforeEach gets a fresh import that picks up the current mock state.
    vi.resetModules();
    const mod = await import("./comments.js");
    const plugin = mod.default();
    middleware = extractMiddleware(plugin as Parameters<typeof extractMiddleware>[0]);
  });

  // -------------------------------------------------------------------------
  // Route: non-API requests are forwarded
  // -------------------------------------------------------------------------

  it("calls next() for non-API paths", () =>
    new Promise<void>((resolve) => {
      const req = makeReq("GET", "/not-api");
      const res = makeRes();
      middleware(req, res, () => resolve());
    }));

  it("calls next() for paths that share the /api/comments prefix but aren't the API", () =>
    new Promise<void>((resolve) => {
      const req = makeReq("GET", "/api/commentsfoo");
      const res = makeRes();
      middleware(req, res, () => resolve());
    }));

  // -------------------------------------------------------------------------
  // GET /api/comments — happy path (all comments)
  // -------------------------------------------------------------------------

  it("GET /api/comments returns all comments sorted newest-first", () =>
    new Promise<void>((resolve, reject) => {
      const older = {
        id: "aaa",
        page: "/foo",
        selectedText: "",
        contextBefore: "",
        contextAfter: "",
        comment: "older",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      const newer = {
        id: "bbb",
        page: "/bar",
        selectedText: "",
        contextBefore: "",
        contextAfter: "",
        comment: "newer",
        createdAt: "2026-06-01T00:00:00.000Z",
      };

      mocks.readdir.mockResolvedValue(["aaa.json", "bbb.json"]);
      mocks.readFile
        .mockResolvedValueOnce(JSON.stringify(older))
        .mockResolvedValueOnce(JSON.stringify(newer));

      const req = makeReq("GET", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as (typeof newer)[];
          expect(body[0].id).toBe("bbb"); // newer first
          expect(body[1].id).toBe("aaa");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // GET /api/comments?page= — happy path (filtered)
  // -------------------------------------------------------------------------

  it("GET /api/comments?page= returns only matching comments", () =>
    new Promise<void>((resolve, reject) => {
      const match = {
        id: "match",
        page: "/docs/foo",
        selectedText: "",
        contextBefore: "",
        contextAfter: "",
        comment: "yes",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      const other = {
        id: "other",
        page: "/docs/bar",
        selectedText: "",
        contextBefore: "",
        contextAfter: "",
        comment: "no",
        createdAt: "2026-01-02T00:00:00.000Z",
      };

      mocks.readdir.mockResolvedValue(["match.json", "other.json"]);
      mocks.readFile
        .mockResolvedValueOnce(JSON.stringify(match))
        .mockResolvedValueOnce(JSON.stringify(other));

      const req = makeReq("GET", "/api/comments?page=%2Fdocs%2Ffoo");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as (typeof match)[];
          expect(body).toHaveLength(1);
          expect(body[0].id).toBe("match");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // GET — error handling (bugfix: must respond 500, not hang)
  // -------------------------------------------------------------------------

  it("GET responds 500 with error JSON when readdir throws", () =>
    new Promise<void>((resolve, reject) => {
      mocks.readdir.mockRejectedValue(new Error("disk on fire"));

      const req = makeReq("GET", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(500);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toMatch(/disk on fire/);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("GET responds 500 with error JSON when readFile throws", () =>
    new Promise<void>((resolve, reject) => {
      mocks.readdir.mockResolvedValue(["abc.json"]);
      mocks.readFile.mockRejectedValue(new Error("permission denied"));

      const req = makeReq("GET", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(500);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toMatch(/permission denied/);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // POST /api/comments — happy path
  // -------------------------------------------------------------------------

  it("POST /api/comments creates a comment and responds 201", () =>
    new Promise<void>((resolve, reject) => {
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, {
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "great text",
      });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(201);
          const body = JSON.parse(res.body) as {
            id: string;
            page: string;
            comment: string;
            createdAt: string;
          };
          expect(body.id).toBeTruthy();
          expect(body.page).toBe("/docs/intro");
          expect(body.comment).toBe("great text");
          expect(body.createdAt).toBeTruthy();
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // POST — zone field (bugfix: zone must be stored and returned)
  // -------------------------------------------------------------------------

  it("POST stores and returns the zone field", () =>
    new Promise<void>((resolve, reject) => {
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, {
        page: "/docs/intro",
        zone: "sidebar",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "zoned",
      });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(201);
          const body = JSON.parse(res.body) as { zone?: string };
          expect(body.zone).toBe("sidebar");

          // Also verify zone was written to disk
          expect(mocks.writeFile).toHaveBeenCalledOnce();
          const [, written] = mocks.writeFile.mock.calls[0] as [string, string];
          const stored = JSON.parse(written) as { zone?: string };
          expect(stored.zone).toBe("sidebar");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("POST omits zone when not provided", () =>
    new Promise<void>((resolve, reject) => {
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, {
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "no zone",
      });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(201);
          const body = JSON.parse(res.body) as { zone?: string };
          expect(body.zone).toBeUndefined();
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // POST — malformed JSON body
  // -------------------------------------------------------------------------

  it("POST responds 400 when request body is malformed JSON", () =>
    new Promise<void>((resolve, reject) => {
      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      req.emit("data", Buffer.from("not valid json{{{"));
      req.emit("end");

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(400);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toBeTruthy();
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // POST — error handling (bugfix: must respond 500, not hang)
  // -------------------------------------------------------------------------

  it("POST responds 500 with error JSON when writeFile throws", () =>
    new Promise<void>((resolve, reject) => {
      mocks.writeFile.mockRejectedValue(new Error("no space left"));

      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, {
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "fail write",
      });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(500);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toMatch(/no space left/);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // PATCH /api/comments?id= — happy path
  // -------------------------------------------------------------------------

  it("PATCH /api/comments?id= updates comment text and responds 200", () =>
    new Promise<void>((resolve, reject) => {
      const existing = {
        id: "abc123",
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "old text",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      mocks.readFile.mockResolvedValue(JSON.stringify(existing));
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("PATCH", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, { comment: "updated text" });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as { comment: string; id: string };
          expect(body.comment).toBe("updated text");
          expect(body.id).toBe("abc123");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("PATCH responds 400 when id is missing", () =>
    new Promise<void>((resolve, reject) => {
      const req = makeReq("PATCH", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(400);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("PATCH responds 400 when request body is malformed JSON", () =>
    new Promise<void>((resolve, reject) => {
      const req = makeReq("PATCH", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      req.emit("data", Buffer.from("not valid json{{{"));
      req.emit("end");

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(400);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toBeTruthy();
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("PATCH responds 500 when writeFile fails after a successful readFile", () =>
    new Promise<void>((resolve, reject) => {
      const existing = {
        id: "abc123",
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "old text",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      mocks.readFile.mockResolvedValue(JSON.stringify(existing));
      mocks.writeFile.mockRejectedValue(new Error("ENOSPC: no space left on device"));

      const req = makeReq("PATCH", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, { comment: "updated text" });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(500);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toMatch(/ENOSPC/);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("PATCH responds 404 when comment file does not exist", () =>
    new Promise<void>((resolve, reject) => {
      mocks.readFile.mockRejectedValue(new Error("ENOENT: no such file"));

      const req = makeReq("PATCH", "/api/comments?id=missing");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, { comment: "new text" });

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(404);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // DELETE /api/comments?id= — happy path
  // -------------------------------------------------------------------------

  it("DELETE /api/comments?id= deletes comment and responds 200", () =>
    new Promise<void>((resolve, reject) => {
      mocks.unlink.mockResolvedValue(undefined);

      const req = makeReq("DELETE", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as { deleted: string };
          expect(body.deleted).toBe("abc123");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("DELETE responds 400 when id is missing", () =>
    new Promise<void>((resolve, reject) => {
      const req = makeReq("DELETE", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(400);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  it("DELETE responds 404 when comment does not exist", () =>
    new Promise<void>((resolve, reject) => {
      const enoent = Object.assign(new Error("ENOENT: no such file or directory"), {
        code: "ENOENT",
      });
      mocks.unlink.mockRejectedValue(enoent);

      const req = makeReq("DELETE", "/api/comments?id=ghost");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(404);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // Unknown method
  // -------------------------------------------------------------------------

  it("responds 405 for unsupported HTTP methods", () =>
    new Promise<void>((resolve, reject) => {
      const req = makeReq("PUT", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(405);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-1. GET — non-.json files alongside .json files are skipped
  // -------------------------------------------------------------------------

  it("GET skips non-.json files in the comments directory", () =>
    new Promise<void>((resolve, reject) => {
      const comment = {
        id: "abc",
        page: "/foo",
        selectedText: "",
        contextBefore: "",
        contextAfter: "",
        comment: "hello",
        createdAt: "2026-01-01T00:00:00.000Z",
      };

      mocks.readdir.mockResolvedValue(["abc.json", ".gitkeep", "README.md"]);
      mocks.readFile.mockResolvedValue(JSON.stringify(comment));

      const req = makeReq("GET", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as (typeof comment)[];
          expect(body).toHaveLength(1);
          expect(body[0].id).toBe("abc");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-2. GET — empty comments directory returns empty array
  // -------------------------------------------------------------------------

  it("GET returns empty array for empty comments directory", () =>
    new Promise<void>((resolve, reject) => {
      mocks.readdir.mockResolvedValue([]);

      const req = makeReq("GET", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as unknown[];
          expect(body).toEqual([]);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-3. DELETE — non-ENOENT filesystem error returns 500
  // -------------------------------------------------------------------------

  it("DELETE responds 500 for non-ENOENT filesystem errors", () =>
    new Promise<void>((resolve, reject) => {
      mocks.unlink.mockRejectedValue(
        Object.assign(new Error("EPERM: operation not permitted"), { code: "EPERM" }),
      );

      const req = makeReq("DELETE", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(500);
          const body = JSON.parse(res.body) as { error: string };
          expect(body.error).toMatch(/EPERM/);
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-4. commentPath sanitization — path-traversal id in PATCH
  // -------------------------------------------------------------------------

  it("PATCH sanitizes path-traversal characters in the id before reading", () =>
    new Promise<void>((resolve, reject) => {
      const sanitizedId = "etcpasswd"; // "../../etc/passwd" with non-alphanumeric stripped
      const existing = {
        id: sanitizedId,
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "original",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      mocks.readFile.mockResolvedValue(JSON.stringify(existing));
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("PATCH", "/api/comments?id=../../etc/passwd");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      sendBody(req, { comment: "updated" });

      setTimeout(() => {
        try {
          const [calledPath] = mocks.readFile.mock.calls[0] as [string];
          expect(calledPath).not.toContain("..");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-5. commentPath sanitization — path-traversal id in DELETE
  // -------------------------------------------------------------------------

  it("DELETE sanitizes path-traversal characters in the id before unlinking", () =>
    new Promise<void>((resolve, reject) => {
      mocks.unlink.mockResolvedValue(undefined);

      const req = makeReq("DELETE", "/api/comments?id=../secret");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      setTimeout(() => {
        try {
          const [calledPath] = mocks.unlink.mock.calls[0] as [string];
          expect(calledPath).not.toContain("..");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-6. POST — body arriving in multiple chunks
  // -------------------------------------------------------------------------

  it("POST assembles a body that arrives in multiple chunks", () =>
    new Promise<void>((resolve, reject) => {
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("POST", "/api/comments");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      const payload = JSON.stringify({
        page: "/docs/chunked",
        selectedText: "chunked text",
        contextBefore: "",
        contextAfter: "",
        comment: "assembled",
      });
      const mid = Math.floor(payload.length / 2);
      req.emit("data", Buffer.from(payload.slice(0, mid)));
      req.emit("data", Buffer.from(payload.slice(mid)));
      req.emit("end");

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(201);
          const body = JSON.parse(res.body) as { comment: string; page: string };
          expect(body.comment).toBe("assembled");
          expect(body.page).toBe("/docs/chunked");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-7. PATCH — body arriving in multiple chunks
  // -------------------------------------------------------------------------

  it("PATCH assembles a body that arrives in multiple chunks", () =>
    new Promise<void>((resolve, reject) => {
      const existing = {
        id: "abc123",
        page: "/docs/intro",
        selectedText: "hello",
        contextBefore: "",
        contextAfter: "",
        comment: "old text",
        createdAt: "2026-01-01T00:00:00.000Z",
      };
      mocks.readFile.mockResolvedValue(JSON.stringify(existing));
      mocks.writeFile.mockResolvedValue(undefined);

      const req = makeReq("PATCH", "/api/comments?id=abc123");
      const res = makeRes();

      middleware(req, res, () => reject(new Error("next() should not be called")));

      const payload = JSON.stringify({ comment: "multi-chunk update" });
      const mid = Math.floor(payload.length / 2);
      req.emit("data", Buffer.from(payload.slice(0, mid)));
      req.emit("data", Buffer.from(payload.slice(mid)));
      req.emit("end");

      setTimeout(() => {
        try {
          expect(res.statusCode).toBe(200);
          const body = JSON.parse(res.body) as { comment: string };
          expect(body.comment).toBe("multi-chunk update");
          resolve();
        } catch (e) {
          reject(e);
        }
      }, 20);
    }));

  // -------------------------------------------------------------------------
  // S-8. Plugin metadata
  // -------------------------------------------------------------------------

  it("exports a plugin with name 'docs-comments' and apply 'serve'", async () => {
    vi.resetModules();
    const mod = await import("./comments.js");
    const plugin = mod.default() as { name: string; apply: string };
    expect(plugin.name).toBe("docs-comments");
    expect(plugin.apply).toBe("serve");
  });
});
