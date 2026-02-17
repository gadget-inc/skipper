import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRouter } from "@remix-run/fetch-router";
import { createRequestListener } from "@remix-run/node-fetch-server";
import { registerRoutes } from "./routes.ts";

const __dirname = dirname(fileURLToPath(import.meta.url));

// Load static files into memory at startup
const htmxJs = readFileSync(join(__dirname, "static", "htmx.min.js"), "utf-8");

const router = createRouter();

// Serve static files
router.get("/static/htmx.min.js", () => {
  return new Response(htmxJs, {
    headers: {
      "content-type": "application/javascript",
      "cache-control": "public, max-age=31536000, immutable",
    },
  });
});

// Register application routes
registerRoutes(router);

const port = parseInt(process.env["PORT"] ?? "3000", 10);

const server = createServer(createRequestListener((request) => router.fetch(request)));

server.listen(port, () => {
  console.log(`web ui listening on http://localhost:${port}`);
});
