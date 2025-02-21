#!/usr/bin/env -S deno run -A
import { sleep } from "npm:zx";

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
    target_cpu_usage_milli: 100,
    target_memory_usage_mib: 200,
  },
};

let requestId = 0;
const failures: ({ requestId: number; status: number; body: string } | { requestId: number; error: unknown })[] = [];

async function sendRequest() {
  try {
    const response = await fetch(routerUrl, {
      method: "POST",
      headers: {
        "x-fusion-function": JSON.stringify(echoFunction),
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
