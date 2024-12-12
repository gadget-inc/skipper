import { sleep } from "npm:zx";

// const url = "http://127.0.0.1:31020";
const url = "http://fusion-router.fusion-development.svc.cluster.local";

let requestId = 0;
const failures: ({ requestId: number; status: number; body: string } | { requestId: number; error: unknown })[] = [];

async function sendRequest() {
  try {
    const response = await fetch(url, {
      method: "POST",
      headers: {
        "x-fusion-tenant": "123",
        "x-fusion-metadata": "secret123",
        "x-fusion-namespace": "fusion-fixtures-development",
        "x-fusion-deployment": "echo",
        "x-fusion-min-instances": "0",
        "x-fusion-max-instances": "5",
        "x-fusion-target-cpu-utilization": "100",
        "x-fusion-target-memory-utilization": "200",
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
