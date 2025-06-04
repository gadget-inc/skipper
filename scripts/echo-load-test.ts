#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import ms from "ms";
import process from "node:process";
import { parseArgs } from "node:util";
import pMap from "p-map";
import { dedent } from "ts-dedent";
import { echoFunction, EchoResponseBody, routerUrl } from "./_echo_utils.ts";
import { isAbortError, unwrap } from "./_utils.ts";

const flags = parseArgs({
  args: process.argv.slice(2),
  options: {
    help: { type: "boolean", default: false },
    concurrency: { type: "string", default: "10" },
    requests: { type: "string", default: "100000" },
    tenants: { type: "string", default: "2" },
  },
});

if (flags.values.help) {
  console.log(dedent`
    Usage:
      echo-load-test [flags]

    Flags:
          --concurrency <number>  Number of concurrent requests (${flags.values.concurrency})
          --requests <number>     Number of requests (${flags.values.requests})
          --tenants <number>      Number of tenants (${flags.values.tenants})
      -h, --help                  Show this help message
  `);
  process.exit(0);
}

const abortController = new AbortController();
process.on("SIGINT", () => {
  abortController.abort();
});

let request = 0;
let failures = 0;
const latencies: number[] & { sorted?: boolean } = [];
const percentiles = [0.5, 0.9, 0.99, 0.999, 1];
const tenants = Array.from({ length: parseInt(flags.values.tenants, 10) }, (_, i) => `tenant${i + 1}`);

try {
  await pMap(Array.from({ length: parseInt(flags.values.requests, 10) }), sendRequest, {
    concurrency: parseInt(flags.values.concurrency, 10),
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
      const body = EchoResponseBody.parse(await response.json());
      const traceId = body.headers["traceparent"]?.split("-")[1];

      if (latency > 100) {
        console.log(
          `request: ${request.toLocaleString().padEnd(flags.values.requests.length)}  status: ${response.status}  traceId: ${traceId}  latency: ${ms(
            latency,
            { long: true },
          )}`,
        );
      }
    } else {
      failures++;
      const body = await response.text().catch(() => "unknown");
      console.error(`request: ${request.toLocaleString().padEnd(flags.values.requests.length)}  status: ${response.status}  body: ${body}`);
    }
  } catch (error) {
    request++;
    if (!isAbortError(error)) {
      failures++;
      console.error(`request: ${request.toLocaleString().padEnd(flags.values.requests.length)}  error: ${String(error)}`);
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
  const lower = unwrap(latencies[Math.floor(idx)], "latencies[lower] is undefined");
  const upper = unwrap(latencies[Math.ceil(idx)], "latencies[upper] is undefined");
  if (lower === upper) {
    return lower;
  }

  return lower + (upper - lower) * (idx - lower);
}
