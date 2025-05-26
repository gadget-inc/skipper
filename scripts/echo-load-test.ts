#!/usr/bin/env -S deno run -A
import { echoFunction, routerUrl } from "./_echo_utils.ts";
// @deno-types="npm:@types/ms"
import ms from "npm:ms";
import pMap from "npm:p-map";
import { parseArgs } from "jsr:@std/cli/parse-args";
import { isAbortError } from "./_utils.ts";
import { dedent } from "npm:ts-dedent";

const flags = parseArgs(Deno.args, {
  boolean: ["help"],
  string: ["concurrency", "requests", "tenants"],
  default: {
    concurrency: "10",
    requests: "100000",
    tenants: "2",
  },
  alias: {
    h: "help",
  },
});

if (flags.help) {
  console.log(dedent`
    Usage:
      echo-load-test [flags]

    Flags:
          --concurrency <number>  Number of concurrent requests (${flags.concurrency})
          --requests <number>     Number of requests (${flags.requests})
          --tenants <number>      Number of tenants (${flags.tenants})
      -h, --help                  Show this help message
  `);
  Deno.exit(0);
}

const abortController = new AbortController();
Deno.addSignalListener("SIGINT", () => {
  abortController.abort();
});

let request = 0;
let failures = 0;
const latencies: number[] & { sorted?: boolean } = [];
const percentiles = [0.5, 0.9, 0.99, 0.999, 1];
const tenants = Array.from({ length: parseInt(flags.tenants, 10) }, (_, i) => `tenant${i + 1}`);

try {
  await pMap(Array.from({ length: parseInt(flags.requests, 10) }), sendRequest, {
    concurrency: parseInt(flags.concurrency, 10),
    signal: abortController.signal,
  });
} catch (error) {
  if (!isAbortError(error)) {
    console.error("\nError:", error);
  }
}

console.log(`\n${(request - failures).toLocaleString()} requests succeeded`);
if (failures > 0) {
  console.error(`${failures.toLocaleString()} requests failed`);
}

console.log("\nPercentiles");
for (const percentile of percentiles) {
  console.log(`${percentile.toString().padStart(6)}  ${ms(getPercentile(percentile), { long: true })}`);
}

async function sendRequest() {
  const tenant = tenants[Math.floor(Math.random() * tenants.length)];

  try {
    const start = performance.now();

    const response = await fetch(routerUrl, {
      method: "POST",
      headers: {
        "x-skipper-function": JSON.stringify({ ...echoFunction, tenant }),
        "content-type": "application/json",
      },
      body: JSON.stringify({ hello: "world" }),
      signal: abortController.signal,
    });

    const latency = performance.now() - start;
    latencies.push(latency);

    request++;

    if (response.ok) {
      const traceId = await response.json().then((body) => body.headers["traceparent"]?.split("-")[1]);

      if (latency > 100) {
        console.log(
          `request: ${request.toLocaleString().padEnd(flags.requests.length)}  status: ${response.status}  traceId: ${traceId}  latency: ${
            ms(latency, { long: true })
          }`,
        );
      }
    } else {
      failures++;
      const body = await response.text().catch(() => "unknown");
      console.error(`request: ${request.toLocaleString().padEnd(flags.requests.length)}  status: ${response.status}  body: ${body}`);
    }
  } catch (error) {
    request++;
    if (!isAbortError(error)) {
      failures++;
      console.error(`request: ${request.toLocaleString().padEnd(flags.requests.length)}  error: ${error}`);
    }
  }
}

function getPercentile(percentile: number): number {
  if (!latencies.sorted) {
    latencies.sort((a, b) => a - b);
    latencies.sorted = true;
  }

  if (latencies.length === 0) {
    return 0;
  }

  const idx = percentile * (latencies.length - 1);
  const lower = Math.floor(idx);
  const upper = Math.ceil(idx);
  if (lower === upper) {
    return latencies[lower];
  }

  return latencies[lower] + (latencies[upper] - latencies[lower]) * (idx - lower);
}
