#!/usr/bin/env -S deno run -A
import { echoFunction, routerUrl } from "./_echo_utils.ts";

const response = await fetch(`${routerUrl}/hello`, {
  method: "POST",
  headers: {
    "x-skipper-function": JSON.stringify(echoFunction),
    "content-type": "application/json",
  },
  body: JSON.stringify({ hello: "world" }),
});

const body = await response.json();
console.log(body);
