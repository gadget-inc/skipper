/**
 * Vite plugin that adds a docs commenting overlay in dev mode.
 *
 * - Exposes a JSON API at /api/comments via dev server middleware
 * - Stores comments as individual JSON files in docs/tmp/comments/
 * - Client script is served directly by Vite from plugins/comments-client.ts
 */

import { readdir, readFile, unlink, writeFile, mkdir } from "node:fs/promises";
import { randomUUID } from "node:crypto";
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

async function readComments(page: string): Promise<Comment[]> {
  await ensureDir();
  const files = await readdir(COMMENTS_DIR);
  const comments: Comment[] = [];

  for (const file of files) {
    if (!file.endsWith(".json")) continue;
    const raw = await readFile(join(COMMENTS_DIR, file), "utf-8");
    const comment = JSON.parse(raw) as Comment;
    if (comment.page === page) comments.push(comment);
  }

  return comments;
}

async function readAllComments(): Promise<Comment[]> {
  await ensureDir();
  const files = await readdir(COMMENTS_DIR);
  const comments: Comment[] = [];

  for (const file of files) {
    if (!file.endsWith(".json")) continue;
    const raw = await readFile(join(COMMENTS_DIR, file), "utf-8");
    comments.push(JSON.parse(raw) as Comment);
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
      const promise = page ? readComments(page) : readAllComments();

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
          ...data,
          id: randomUUID(),
          createdAt: new Date().toISOString(),
        };

        void ensureDir()
          .then(() =>
            writeFile(join(COMMENTS_DIR, `${comment.id}.json`), JSON.stringify(comment, null, 2)),
          )
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
        .catch(() => {
          res.writeHead(404, { "content-type": "application/json" });
          res.end(JSON.stringify({ error: "not found" }));
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
