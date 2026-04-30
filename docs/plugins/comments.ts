/**
 * Vite plugin that adds a docs commenting overlay in dev mode.
 *
 * - Exposes a JSON API at /api/comments via dev server middleware
 * - Stores comments as individual JSON files in docs/tmp/comments/
 * - Client script is served directly by Vite from plugins/comments-client.ts
 */

import { randomUUID } from "node:crypto";
import { readdir, readFile, unlink, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";

import type { Plugin, ViteDevServer } from "vite";

const COMMENTS_DIR = join(import.meta.dirname, "..", "tmp", "comments");

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

function commentPath(id: string): string {
  const sanitized = id.replace(/[^a-zA-Z0-9-]/g, "");
  return join(COMMENTS_DIR, `${sanitized}.json`);
}

async function ensureDir(): Promise<void> {
  await mkdir(COMMENTS_DIR, { recursive: true });
}

async function loadComments(page?: string): Promise<Comment[]> {
  await ensureDir();
  const files = await readdir(COMMENTS_DIR);
  const comments: Comment[] = [];

  for (const file of files) {
    if (!file.endsWith(".json")) continue;
    const raw = await readFile(join(COMMENTS_DIR, file), "utf-8");
    const comment = JSON.parse(raw) as Comment;
    if (page === undefined || comment.page === page) comments.push(comment);
  }

  comments.sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime());

  return comments;
}

function addMiddleware(server: ViteDevServer): void {
  server.middlewares.use((req, res, next) => {
    const url = new URL(req.url ?? "/", "http://localhost");
    if (url.pathname !== "/api/comments") return next();

    if (req.method === "GET") {
      const page = url.searchParams.get("page");
      const promise = loadComments(page ?? undefined);

      void promise
        .then((comments) => {
          res.writeHead(200, { "content-type": "application/json" });
          res.end(JSON.stringify(comments));
        })
        .catch((err: unknown) => {
          res.writeHead(500, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: String(err) }));
        });
      return;
    }

    if (req.method === "POST") {
      let body = "";
      req.on("data", (chunk: Buffer) => (body += chunk));
      req.on("end", () => {
        let data: Omit<Comment, "id" | "createdAt">;
        try {
          data = JSON.parse(body) as Omit<Comment, "id" | "createdAt">;
        } catch (err: unknown) {
          res.writeHead(400, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: String(err) }));
          return;
        }

        const comment: Comment = {
          page: data.page,
          zone: data.zone,
          selectedText: data.selectedText,
          contextBefore: data.contextBefore,
          contextAfter: data.contextAfter,
          comment: data.comment,
          id: randomUUID(),
          createdAt: new Date().toISOString(),
        };

        void ensureDir()
          .then(() => writeFile(commentPath(comment.id), JSON.stringify(comment, null, 2)))
          .then(() => {
            res.writeHead(201, { "content-type": "application/json" });
            res.end(JSON.stringify(comment));
          })
          .catch((err: unknown) => {
            res.writeHead(500, { "content-type": "application/json" });
            res.end(JSON.stringify({ error: String(err) }));
          });
      });
      return;
    }

    if (req.method === "PATCH") {
      const id = url.searchParams.get("id");
      if (!id) {
        res.writeHead(400, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: "id parameter required" }));
        return;
      }

      let body = "";
      req.on("data", (chunk: Buffer) => (body += chunk));
      req.on("end", () => {
        let newText: string;
        try {
          ({ comment: newText } = JSON.parse(body) as { comment: string });
        } catch (err: unknown) {
          res.writeHead(400, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: String(err) }));
          return;
        }

        const filePath = commentPath(id);

        readFile(filePath, "utf-8")
          .then(
            (raw) => {
              const existing = JSON.parse(raw) as Comment;
              const updated: Comment = { ...existing, comment: newText };
              return writeFile(filePath, JSON.stringify(updated, null, 2)).then(() => updated);
            },
            () => {
              res.writeHead(404, { "content-type": "application/json" });
              res.end(JSON.stringify({ error: "not found" }));
              return null;
            },
          )
          .then((updated) => {
            if (updated === null) return;
            res.writeHead(200, { "content-type": "application/json" });
            res.end(JSON.stringify(updated));
          })
          .catch((err: unknown) => {
            res.writeHead(500, { "content-type": "application/json" });
            res.end(JSON.stringify({ error: String(err) }));
          });
      });
      return;
    }

    if (req.method === "DELETE") {
      const id = url.searchParams.get("id");
      if (!id) {
        res.writeHead(400, { "content-type": "application/json" });
        res.end(JSON.stringify({ error: "id parameter required" }));
        return;
      }

      unlink(commentPath(id))
        .then(() => {
          res.writeHead(200, { "content-type": "application/json" });
          res.end(JSON.stringify({ deleted: id }));
        })
        .catch((err: unknown) => {
          if (typeof err === "object" && err !== null && (err as NodeJS.ErrnoException).code === "ENOENT") {
            res.writeHead(404, { "content-type": "application/json" });
            res.end(JSON.stringify({ error: "not found" }));
          } else {
            res.writeHead(500, { "content-type": "application/json" });
            res.end(JSON.stringify({ error: String(err) }));
          }
        });
      return;
    }

    res.writeHead(405);
    res.end();
  });
}

export default function commentsPlugin(): Plugin {
  return {
    name: "docs-comments",
    apply: "serve",

    configureServer(server) {
      addMiddleware(server);
    },
  };
}
