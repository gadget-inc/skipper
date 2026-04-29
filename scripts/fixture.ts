#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import process from "node:process";
import { parseArgs } from "node:util";

import ms from "ms";
import pMap from "p-map";
import { dedent } from "ts-dedent";
import WebSocket from "ws";
import { chalk } from "zx";

import { fixtureFunction, FixtureResponseBody, routerUrl } from "./_fixture_utils.ts";
import { isAbortError } from "./_utils.ts";

// -- Helpers --

function parsePositiveInt(value: string, name: string): number {
  const n = parseInt(value, 10);
  if (!Number.isFinite(n) || n <= 0 || String(n) !== value) {
    throw new Error(`Invalid ${name}: ${value} (must be a positive integer)`);
  }
  return n;
}

function parseNonNegativeInt(value: string, name: string): number {
  const n = parseInt(value, 10);
  if (!Number.isFinite(n) || n < 0 || String(n) !== value) {
    throw new Error(`Invalid ${name}: ${value} (must be a non-negative integer)`);
  }
  return n;
}

/** Format a latency value with appropriate precision. */
export function formatLatency(value: number): string {
  if (value < 1) return `${value.toFixed(2)}ms`;
  if (value < 10) return `${value.toFixed(1)}ms`;
  if (value < 1000) return `${Math.round(value)}ms`;
  return `${(value / 1000).toFixed(1)}s`;
}

/** Compute a percentile from a sorted array using linear interpolation. */
export function getPercentile(sorted: number[], p: number): number {
  if (sorted.length === 0) return 0;
  const index = p * (sorted.length - 1);
  const lower = Math.floor(index);
  const upper = Math.ceil(index);
  if (lower === upper) return sorted[lower]!;
  const weight = index - lower;
  return sorted[lower]! + (sorted[upper]! - sorted[lower]!) * weight;
}

// -- request --

export async function request(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      method: { type: "string", default: "POST", short: "m" },
      path: { type: "string", default: "/hello", short: "p" },
      body: { type: "string", default: '{"hello":"world"}', short: "b" },
      tenant: { type: "string", default: fixtureFunction.tenant, short: "t" },
      count: { type: "string", default: "1", short: "n" },
      verbose: { type: "boolean", default: false, short: "v" },
      help: { type: "boolean", default: false, short: "h" },
    },
  });

  if (flags.values.help) {
    console.log(dedent`
      fixture request [flags]

        Send HTTP request(s) to the fixture.

      Flags:
        -m, --method=<verb>     HTTP method (default: POST)
        -p, --path=<path>       Request path (default: /hello)
        -b, --body=<json>       Request body JSON (default: {"hello":"world"})
        -t, --tenant=<id>       Tenant ID (default: ${fixtureFunction.tenant})
        -n, --count=<n>         Number of requests to send (default: 1)
        -v, --verbose           Show response headers (default: false)
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  const count = parsePositiveInt(flags.values.count!, "count");
  const fn = { ...fixtureFunction, tenant: flags.values.tenant! };

  for (let i = 0; i < count; i++) {
    if (count > 1) {
      console.log(chalk.dim(`--- request ${i + 1}/${count} ---`));
    }

    const start = performance.now();
    const response = await fetch(`${routerUrl}${flags.values.path}`, {
      method: flags.values.method,
      headers: {
        "x-skipper-function": JSON.stringify(fn),
        "content-type": "application/json",
      },
      body: flags.values.method !== "GET" ? flags.values.body : null,
    });
    const latency = performance.now() - start;

    const status = response.ok ? chalk.green(response.status) : chalk.red(response.status);
    console.log(`${flags.values.method} ${flags.values.path}  ${status}  ${formatLatency(latency)}`);

    if (flags.values.verbose) {
      for (const [key, value] of response.headers) {
        console.log(chalk.dim(`  ${key}: ${value}`));
      }
    }

    if (response.ok) {
      const body = FixtureResponseBody.parse(await response.json());
      console.log(body);
    } else {
      const text = await response.text().catch(() => "");
      if (text) console.error(chalk.red(text));
    }

    if (count > 1 && i < count - 1) console.log();
  }
}

// -- websocket --

export async function websocket(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      message: { type: "string", default: "ping" },
      interval: { type: "string", default: "1000", short: "i" },
      count: { type: "string", default: "0", short: "n" },
      tenant: { type: "string", default: fixtureFunction.tenant, short: "t" },
      help: { type: "boolean", default: false, short: "h" },
    },
  });

  if (flags.values.help) {
    console.log(dedent`
      fixture websocket [flags]

        Open a WebSocket connection to the fixture.

      Flags:
            --message=<text>    Message to send (default: ping)
        -i, --interval=<ms>     Interval between messages in ms (default: 1000)
        -n, --count=<n>         Number of messages to send, 0=unlimited (default: 0)
        -t, --tenant=<id>       Tenant ID (default: ${fixtureFunction.tenant})
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  const interval = parsePositiveInt(flags.values.interval!, "interval");
  const maxCount = parseNonNegativeInt(flags.values.count!, "count");
  const fn = { ...fixtureFunction, tenant: flags.values.tenant! };
  let sent = 0;

  const abortController = new AbortController();
  process.on("SIGINT", () => abortController.abort());

  return new Promise<void>((resolve) => {
    const socket = new WebSocket(routerUrl, undefined, {
      headers: { "x-skipper-function": JSON.stringify(fn) },
    });

    abortController.signal.addEventListener("abort", () => {
      console.log(chalk.dim("\nClosing..."));
      socket.close();
    });

    socket.on("open", () => {
      console.log(chalk.green("connected"));
      socket.send(flags.values.message!);
      sent++;
    });

    socket.on("message", async (data) => {
      // oxlint-disable-next-line typescript/no-base-to-string
      const message = data.toString("utf8");
      console.log(`${chalk.dim("recv")}  ${message}`);

      if (maxCount > 0 && sent >= maxCount) {
        socket.close();
        return;
      }

      if (!abortController.signal.aborted) {
        await new Promise((r) => setTimeout(r, interval));
        if (!abortController.signal.aborted) {
          socket.send(flags.values.message!);
          sent++;
          console.log(`${chalk.dim("send")}  ${flags.values.message}`);
        }
      }
    });

    socket.on("error", (error) => {
      console.error(chalk.red("error"), error.message);
    });

    socket.on("close", (code) => {
      console.log(chalk.dim(`closed (${code})`));
      console.log(`${sent} message(s) sent`);
      resolve();
    });
  });
}

// -- load --

const PERCENTILES = [0.5, 0.9, 0.99, 0.999, 1] as const;
const PROGRESS_INTERVAL_MS = 250;

export async function load(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      concurrency: { type: "string", default: "10", short: "c" },
      requests: { type: "string", default: "100000", short: "n" },
      infinite: { type: "boolean", default: false },
      duration: { type: "string", default: "", short: "d" },
      tenants: { type: "string", default: "2", short: "t" },
      "no-warm-up": { type: "boolean", default: false },
      help: { type: "boolean", default: false, short: "h" },
    },
  });

  if (flags.values.help) {
    console.log(dedent`
      fixture load [flags]

        Run a load test against the fixture.

      Flags:
        -c, --concurrency=<n>   Concurrent requests (default: 10)
        -n, --requests=<n>      Total requests to send (default: 100,000)
        -d, --duration=<time>   Run for a duration instead of request count (e.g. 30s, 1m)
            --infinite          Run until interrupted (Ctrl+C)
        -t, --tenants=<n>       Number of tenants to rotate through (default: 2)
            --no-warm-up        Skip warm-up (by default, one request per tenant is sent)
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  const concurrency = parsePositiveInt(flags.values.concurrency!, "concurrency");
  const tenantCount = parsePositiveInt(flags.values.tenants!, "tenants");
  const skipWarmUp = flags.values["no-warm-up"];
  let durationMs = 0;
  if (flags.values.duration) {
    // ms() returns undefined for invalid input — fail early with a clear message.
    const parsed = ms(flags.values.duration as Parameters<typeof ms>[0]);
    if (typeof parsed !== "number" || !Number.isFinite(parsed) || parsed <= 0) {
      throw new Error(`Invalid duration: ${flags.values.duration} (e.g. 30s, 1m, 5m)`);
    }
    durationMs = parsed;
  }
  const totalRequests = flags.values.infinite || durationMs ? Infinity : parseInt(flags.values.requests!, 10);
  const tenants = Array.from({ length: tenantCount }, (_, i) => `tenant-${i + 1}`);

  const abortController = new AbortController();
  process.on("SIGINT", () => abortController.abort());

  // -- Warm-up: send one request per tenant to ensure each has an assigned pod --
  if (!skipWarmUp) {
    for (let i = 0; i < tenants.length; i++) {
      const tenant = tenants[i]!;
      process.stdout.write(chalk.dim(`\r\x1b[KWarming up ${tenant} (${i + 1}/${tenants.length})...`));
      await fetch(routerUrl, {
        method: "POST",
        headers: {
          "x-skipper-function": JSON.stringify({ ...fixtureFunction, tenant }),
          "content-type": "application/json",
        },
        body: '{"hello":"world"}',
        signal: abortController.signal,
      }).catch(() => {});
    }
    process.stdout.write(chalk.dim(`\r\x1b[KWarmed up ${tenants.length} tenant(s)\n`));
  }

  // -- Header --
  const requestsLabel = flags.values.infinite
    ? "infinite"
    : durationMs
      ? `duration ${flags.values.duration}`
      : `${totalRequests.toLocaleString()} requests`;
  console.log(`\n${chalk.bold("Load test")}  ${concurrency} concurrent, ${requestsLabel}, ${tenantCount} tenant(s)`);
  console.log(`${chalk.dim("Target")}     ${routerUrl}\n`);

  // -- State --
  // Running counters for O(1) memory stats.
  let completed = 0;
  let failures = 0;
  let minLatency = Infinity;
  let maxLatency = -Infinity;
  let sumLatency = 0;

  // Reservoir sampling: fixed-size buffer for percentile estimation.
  // Uses Algorithm R — each incoming sample has a (reservoirSize / completed)
  // probability of being kept, giving a uniform random sample of all latencies.
  const RESERVOIR_SIZE = 10_000;
  const reservoir: number[] = [];
  let reservoirCount = 0; // total samples seen (may exceed reservoir length)

  function reservoirAdd(value: number): void {
    reservoirCount++;
    if (reservoir.length < RESERVOIR_SIZE) {
      reservoir.push(value);
    } else {
      const j = Math.floor(Math.random() * reservoirCount);
      if (j < RESERVOIR_SIZE) {
        reservoir[j] = value;
      }
    }
  }

  const testStart = performance.now();
  const deadline = durationMs ? testStart + durationMs : 0;

  // -- Progress --
  const isTTY = process.stdout.isTTY;
  const progressTimer = isTTY
    ? setInterval(() => {
        const elapsed = (performance.now() - testStart) / 1000;
        const rps = completed / elapsed;
        const sorted = [...reservoir].sort((a, b) => a - b);
        const p99 = sorted.length > 0 ? formatLatency(getPercentile(sorted, 0.99)) : "-";
        const elapsedLabel = ms(Math.round(performance.now() - testStart));
        const progress = flags.values.infinite
          ? `${completed.toLocaleString()} (${elapsedLabel})`
          : durationMs
            ? `${elapsedLabel}/${flags.values.duration}`
            : `${completed.toLocaleString()}/${totalRequests.toLocaleString()}`;
        process.stdout.write(`\r\x1b[K  ${progress}  ${Math.round(rps).toLocaleString()} req/s  ${failures} errors  p99: ${p99}`);
      }, PROGRESS_INTERVAL_MS)
    : undefined;

  // -- Generate work items --
  function* generateWork(): Generator<number> {
    let i = 0;
    while (true) {
      if (!durationMs && i >= totalRequests) return;
      if (durationMs && performance.now() >= deadline) return;
      if (abortController.signal.aborted) return;
      yield i++;
    }
  }

  // -- Send requests --
  async function sendRequest(): Promise<void> {
    const tenant = tenants[Math.floor(Math.random() * tenants.length)]!;
    const start = performance.now();

    try {
      const response = await fetch(routerUrl, {
        method: "POST",
        headers: {
          "x-skipper-function": JSON.stringify({ ...fixtureFunction, tenant }),
          "content-type": "application/json",
        },
        body: '{"hello":"world"}',
        signal: abortController.signal,
      });

      const latency = performance.now() - start;
      completed++;
      sumLatency += latency;
      if (latency < minLatency) minLatency = latency;
      if (latency > maxLatency) maxLatency = latency;
      reservoirAdd(latency);

      if (!response.ok) {
        failures++;
      }
    } catch (error) {
      if (isAbortError(error)) return;
      completed++;
      failures++;
    }
  }

  try {
    await pMap(generateWork(), sendRequest, {
      concurrency,
      signal: abortController.signal,
    });
  } catch (error) {
    if (!isAbortError(error)) throw error;
  }

  if (progressTimer) clearInterval(progressTimer);
  if (isTTY) process.stdout.write("\r\x1b[K");

  // -- Summary --
  const elapsed = performance.now() - testStart;
  const succeeded = completed - failures;
  const rps = completed / (elapsed / 1000);
  const sorted = [...reservoir].sort((a, b) => a - b);

  console.log(chalk.bold("Results\n"));

  const errorRate = completed > 0 ? ((failures / completed) * 100).toFixed(2) : "0.00";
  console.log(
    `  Requests     ${completed.toLocaleString()} total, ${chalk.green(succeeded.toLocaleString())} succeeded` +
      (failures > 0 ? `, ${chalk.red(failures.toLocaleString())} failed (${errorRate}%)` : ""),
  );
  console.log(`  Duration     ${formatLatency(elapsed)}`);
  console.log(`  Throughput   ${chalk.bold(Math.round(rps).toLocaleString())} req/s`);

  if (sorted.length > 0) {
    console.log(`\n  Latency`);
    for (const p of PERCENTILES) {
      const label = p === 1 ? "max" : `p${p * 100}`;
      // Use the exact running max counter; reservoir for all other percentiles.
      const value = p === 1 ? maxLatency : getPercentile(sorted, p);
      console.log(`    ${label.padEnd(8)} ${formatLatency(value)}`);
    }
  }

  console.log();

  if (failures > 0) {
    process.exitCode = 1;
  }
}

// -- Entry point --

if (import.meta.filename === process.argv[1]) {
  const subcommand = process.argv[2];

  if (!subcommand || subcommand === "--help" || subcommand === "-h") {
    console.log(dedent`
      fixture <command> [flags]

        Test the fixture via HTTP and WebSocket.

      Commands:
        request [flags]     Send HTTP request(s) to the fixture
        websocket [flags]   Open a WebSocket connection to the fixture
        load [flags]        Run a load test against the fixture

      Run 'fixture <command> --help' for command-specific flags.
    `);
    process.exit(0);
  }

  switch (subcommand) {
    case "request":
      await request(process.argv.slice(3));
      break;
    case "websocket":
    case "ws":
      await websocket(process.argv.slice(3));
      break;
    case "load":
      await load(process.argv.slice(3));
      break;
    default:
      console.error(`Unknown command: ${subcommand}`);
      console.error(`Run 'fixture --help' for usage.`);
      process.exit(1);
  }
}
