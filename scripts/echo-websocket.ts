#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import ms from "ms";
import { setTimeout } from "node:timers/promises";
import WebSocket from "ws";
import { echoFunction, routerUrl } from "./_echo_utils.ts";

const socket = new WebSocket(routerUrl, undefined, {
  headers: {
    "x-skipper-function": JSON.stringify(echoFunction),
  },
});

socket.on("open", () => {
  console.log("websocket open");
  socket.send("ping");
});

socket.on("message", async (data, isBinary) => {
  // eslint-disable-next-line @typescript-eslint/no-base-to-string
  const message = data.toString("utf8");
  console.log("websocket message", { message, isBinary });
  if (message === "pong") {
    await setTimeout(ms("1s"));
    socket.send("ping");
  }
});

socket.on("error", (event) => {
  console.log("websocket error", event);
});

socket.on("close", (event) => {
  console.log("websocket close", event);
});
