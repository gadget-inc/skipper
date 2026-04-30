#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { generateKeyPairSync } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import process from "node:process";
import { parseArgs } from "node:util";

import { dedent } from "ts-dedent";
import { $ } from "zx";

import { abs, expandSkipperAlias, isAbortError } from "./_utils.ts";
import { createGoWatcher, watchAndRun } from "./_watch.ts";

$.cwd = abs();

const flags = parseArgs({
  args: process.argv.slice(2),
  options: {
    only: { type: "string", default: "controller,router,docs" },
    help: { type: "boolean", default: false, short: "h" },
  },
});

flags.values.only = expandSkipperAlias(flags.values.only!);

if (flags.values.help) {
  console.log(dedent`
    Usage:
      dev [flags]

    Flags:
          --only <string>  Components to run (default: controller,router,docs)
      -h, --help           Show this help message

    Local endpoints:
      Controller gRPC: 127.0.0.1:50051
      Router:          http://127.0.0.1:8081
      Docs:            http://localhost:4321

    First-time setup:
      deploy --only=fixtures,metrics-server
  `);
  process.exit(0);
}

const components = new Set(flags.values.only!.split(","));

// Generate PASETO keypair if missing and controller is enabled.
// Check for both files — a partial write (e.g. killed between writes) should regenerate.
if (components.has("controller") && (!existsSync(abs("tmp/paseto/private.pem")) || !existsSync(abs("tmp/paseto/public.pem")))) {
  await mkdir(abs("tmp/paseto"), { recursive: true });
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  await writeFile(abs("tmp/paseto/private.pem"), privateKey.export({ format: "pem", type: "pkcs8" }));
  await writeFile(abs("tmp/paseto/public.pem"), publicKey.export({ format: "pem", type: "spki" }));
  console.log("Generated PASETO keypair in tmp/paseto/");
}

// Check that fixture namespace exists
if (components.has("controller")) {
  const nsCheck = await $`kubectl get namespace skipper-development-fixtures`.nothrow().quiet();
  if (nsCheck.exitCode !== 0) {
    console.warn("⚠ Namespace skipper-development-fixtures not found. Run: deploy --only=fixtures,metrics-server");
  }
}

const ac = new AbortController();
process.on("SIGINT", () => ac.abort());
process.on("SIGTERM", () => ac.abort());

const processes: Promise<unknown>[] = [];

const needsGoWatch = components.has("controller") || components.has("router");
const goWatcher = needsGoWatch ? createGoWatcher(abs(), ac.signal) : { subscribe() {} };

if (components.has("controller") || components.has("router")) {
  await mkdir(abs("tmp/logs"), { recursive: true });
}

if (components.has("controller")) {
  const pasetoKey = await readFile(abs("tmp/paseto/private.pem"), "utf8");
  const env = {
    ...process.env,
    SKIPPER_NAMESPACE: "skipper-development",
    SKIPPER_POD_IP: "127.0.0.1",
    SKIPPER_PASETO_PRIVATE_KEY: pasetoKey,
    SKIPPER_FUNCTION_NAMESPACES: "skipper-development-fixtures,skipper-test-fixtures",
    SKIPPER_SINGLE_CONTROLLER_MODE: "true",
    SKIPPER_HOST: "127.0.0.1",
    SKIPPER_LOG_FORMAT: "text",
    SKIPPER_LOG_FILE: abs("tmp/logs/controller.jsonl"),
    SKIPPER_LOG_FILE_LEVEL: "debug",
    SKIPPER_LOG_FILE_FORMAT: "json",
    SKIPPER_SKIP_FORBIDDEN_NAMESPACES: "true",
  };
  processes.push(watchAndRun("controller", () => $({ env })`go run ./cmd/controller`, goWatcher, ac.signal));
}

if (components.has("router")) {
  const env = {
    ...process.env,
    SKIPPER_POD_IP: "127.0.0.1",
    SKIPPER_CONTROLLER_SERVICE_HOST: "127.0.0.1",
    SKIPPER_HOST: "127.0.0.1",
    SKIPPER_PORT: "8081",
    SKIPPER_LOG_FORMAT: "text",
    SKIPPER_LOG_FILE: abs("tmp/logs/router.jsonl"),
    SKIPPER_LOG_FILE_LEVEL: "debug",
    SKIPPER_LOG_FILE_FORMAT: "json",
  };
  processes.push(watchAndRun("router", () => $({ env })`go run ./cmd/router`, goWatcher, ac.signal));
}

if (components.has("docs")) {
  processes.push(
    $({ signal: ac.signal, cwd: abs("docs") })`pnpm astro dev`.exitCode.catch((error) => {
      if (!isAbortError(error)) throw error;
    }),
  );
}

// Print active endpoints
if (components.has("controller")) {
  console.log("Controller gRPC: 127.0.0.1:50051");
}
if (components.has("router")) {
  console.log("Router:          http://127.0.0.1:8081");
}
if (components.has("docs")) {
  console.log("Docs:            http://localhost:4321");
}

await Promise.allSettled(processes);
