#!/usr/bin/env -S deno run -A
import { sleep } from "npm:zx";
import { echoFunction, routerUrl } from "./_echo_utils.ts";

let requestId = 0;
const failures: ({ requestId: number; status: number; body: string } | { requestId: number; error: unknown })[] = [];
const tenants = Array.from({ length: 5 }, (_, i) => `tenant${i + 1}`);

async function sendRequest() {
  try {
    const response = await fetch(routerUrl, {
      method: "POST",
      headers: {
        "x-skipper-function": JSON.stringify({ ...echoFunction, tenant: tenants[Math.floor(Math.random() * tenants.length)] }),
        "content-type": "application/json",
      },
      body: JSON.stringify({ hello: "world" }),
    });

    if (response.ok) {
      console.log("request", ++requestId, "status", response.status);
    } else {
      console.error("request", ++requestId, "status", response.status);
      failures.push({ requestId: requestId, status: response.status, body: await response.text() });
    }
  } catch (error) {
    console.error("request", ++requestId, "error", error);
    failures.push({ requestId: requestId, error });
  }
}

const abortController = new AbortController();
Deno.addSignalListener("SIGINT", () => {
  abortController.abort();
});

const responses = [];
while (!abortController.signal.aborted) {
  responses.push(sendRequest());
  await sleep(1);
}

await Promise.all(responses);

if (failures.length > 0) {
  console.error("Failures:");
  console.dir(failures, { depth: null });
} else {
  console.log("All requests succeeded");
}
