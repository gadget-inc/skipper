const WebSocket = require("ws");
const { setTimeout } = require("timers/promises");

const url = "http://fusion-router.fusion-development.svc.cluster.local:8080";

const socket = new WebSocket(url, undefined, {
  headers: {
    "x-fusion-tenant": "123",
    "x-fusion-namespace": "example-development",
    "x-fusion-deployment": "example-deno",
    "x-fusion-metadata": "secret123",
    "x-fusion-min-replicas": "0",
    "x-fusion-max-replicas": "5",
    "x-fusion-target-cpu-utilization": "100",
    "x-fusion-target-memory-utilization": "200",
  },
});

socket.on("open", (event) => {
  console.log("websocket open", event);
  socket.send("ping");
});

socket.on("message", async (message) => {
  const data = message.toString();

  console.log("websocket message", { data });
  if (data === "pong") {
    await setTimeout(1000);
    socket.send("ping");
  }
});

socket.on("error", (event) => {
  console.log("websocket error", event);
});

socket.on("close", (event) => {
  console.log("websocket close", event);
});
