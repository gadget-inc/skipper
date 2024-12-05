// @deno-types="npm:@types/ws"
import WebSocket from "npm:ws";
import { delay } from "jsr:@std/async";

// const url = "http://127.0.0.1:31020";
const url = "http://fusion-router.fusion-development.svc.cluster.local";

const socket = new WebSocket(url, undefined, {
  headers: {
    "x-fusion-tenant": "123",
    "x-fusion-namespace": "fusion-fixtures-development",
    "x-fusion-deployment": "echo",
    "x-fusion-metadata": "secret123",
    "x-fusion-min-instances": "0",
    "x-fusion-max-instances": "5",
    "x-fusion-target-cpu-utilization": "100",
    "x-fusion-target-memory-utilization": "200",
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
