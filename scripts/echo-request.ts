#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { echoFunction, EchoResponseBody, routerUrl } from "./_echo_utils.ts";

const response = await fetch(`${routerUrl}/hello`, {
  method: "POST",
  headers: {
    "x-skipper-function": JSON.stringify(echoFunction),
    "content-type": "application/json",
  },
  body: JSON.stringify({ hello: "world" }),
});

const body = EchoResponseBody.parse(await response.json());
console.log(body);
