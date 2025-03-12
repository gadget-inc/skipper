#!/usr/bin/env -S deno run -A
// @deno-types="npm:@types/ws"
import WebSocket from "npm:ws";
import { delay } from "jsr:@std/async";
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
