#!/usr/bin/env -S deno run -A
// @deno-types="npm:@types/ws"
import WebSocket from "npm:ws";
import { delay } from "jsr:@std/async";

const routerUrl = "http://127.0.0.1:31020";
// const routerUrl = "http://fusion-development-router.fusion-development.svc.cluster.local";

const echoFunction = {
  namespace: "fusion-fixtures-development",
  deployment: "echo",
  tenant: "123",
  metadata: JSON.stringify({ foo: "bar" }),
  scale: {
    min_instances: 0,
    max_instances: 5,
    target_cpu_usage: 100,
    target_memory_usage: 200,
  },
};

const socket = new WebSocket(routerUrl, undefined, {
  headers: {
    "x-fusion-function": JSON.stringify(echoFunction),
  },
});

socket.on("open", () => {
  console.log("websocket open");
  socket.send("ping");
});

socket.on("message", async (message) => {
  const data = message.toString();

  console.log("websocket message", { data });
  if (data === "pong") {
    await delay(1000);
    socket.send("ping");
  }
});

socket.on("error", (event) => {
  console.log("websocket error", event);
});

socket.on("close", (event) => {
  console.log("websocket close", event);
});
