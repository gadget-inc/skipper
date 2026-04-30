import "./telemetry.ts";
import assert from "node:assert";
import { stat, writeFile } from "node:fs/promises";
import { createServer } from "node:http";

import { V2 } from "paseto";
import { pino } from "pino";
import pretty from "pino-pretty";
import { WebSocketServer } from "ws";

const log = pino(pretty());

const tokenFilepath = "/tmp/token";
let assigned = await stat(tokenFilepath)
  .then(() => true)
  .catch(() => false);

const server = createServer(async (request, response) => {
  if (request.method === "GET" && request.url === "/healthz") {
    response.end();
    return;
  }

  log.info({ method: request.method, url: request.url, headers: request.headers }, "incoming request");
  if (request.method === "POST" && request.url === "/__skipper/assign") {
    if (assigned) {
      response.statusCode = 409;
      response.end("already assigned");
      return;
    }

    assert(process.env["SKIPPER_PUBLIC_KEY"], "SKIPPER_PUBLIC_KEY is not set");
    assert(typeof request.headers["x-skipper-token"] === "string", "x-skipper-token is not set");
    assert(typeof request.headers["x-skipper-function"] === "string", "x-skipper-function is not set");

    assigned = true;
    const token = request.headers["x-skipper-token"];
    const payload = await V2.verify(token, process.env["SKIPPER_PUBLIC_KEY"]);
    const fn = JSON.parse(request.headers["x-skipper-function"]) as unknown;
    await writeFile(tokenFilepath, token);
    log.info({ token, payload, fn }, "assigned");
    response.end();
    return;
  }

  if (!assigned) {
    response.statusCode = 503;
    response.end("not assigned");
    return;
  }

  const body = await new Promise((resolve, reject) => {
    let body = "";
    request.on("data", (chunk) => (body += String(chunk)));
    request.on("end", () => resolve(body));
    request.on("error", (error) => reject(error));
  });

  response.setHeader("content-type", "application/json");
  response.end(JSON.stringify({ method: request.method, url: request.url, headers: request.headers, body }));
});

const wss = new WebSocketServer({ server });

wss.on("connection", (socket) => {
  socket.addEventListener("open", () => {
    log.info("websocket opened");
  });

  socket.addEventListener("message", (event) => {
    log.info({ data: event.data }, "websocket message");
    if (event.data === "ping") {
      socket.send("pong");
    }
  });
});

server.listen(8888, () => {
  log.info("server listening on port 8888");
});

for (const signal of ["SIGTERM", "SIGINT"]) {
  process.once(signal, () => {
    log.info("shutting down");
    wss.close();
    server.close();
  });
}

await new Promise((resolve) => server.on("close", resolve));
log.info("done");
