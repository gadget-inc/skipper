#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { mkdir } from "node:fs/promises";
import process from "node:process";

import { $ } from "zx";

import { $nothrow, abs, isCI } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

export async function runGo(argv: string[]) {
  await mkdir(abs("tmp/logs"), { recursive: true });

  const goTestFlags = [...argv];

  function goTestFlagsHas(flag: string) {
    return goTestFlags.some((arg) => arg.startsWith(flag));
  }

  if (!goTestFlagsHas("./")) {
    // run all tests if a path wasn't provided
    goTestFlags.unshift("./...");
  }

  if (isCI) {
    if (!goTestFlagsHas("-count")) {
      // ensure test results are not cached in CI
      goTestFlags.push("-count=1");
    }
    if (!goTestFlagsHas("-race")) {
      // ensure race detection is enabled in CI
      goTestFlags.push("-race");
    }
  }

  const gotestsumFlags = ["--format-hide-empty-pkg"];
  if (!isCI) {
    gotestsumFlags.push("--hide-summary=skipped");
  }

  await $nothrow`gotestsum ${gotestsumFlags} -- ${goTestFlags} | tee tmp/logs/tests.log`;

  if (isCI) {
    // run allocation tests without race detector since it adds extra allocations
    await $nothrow`gotestsum ${gotestsumFlags} -- ./... -run=Allocations -count=1 | tee -a tmp/logs/tests.log`;
  }
}

export async function runDocs(argv: string[]) {
  await $nothrow`pnpm --filter docs test ${argv}`;
}

export async function runScripts(argv: string[]) {
  await $nothrow`pnpm --filter scripts test ${argv}`;
}

export async function runE2e(argv: string[]) {
  await $nothrow`go test ./e2e/... ${argv}`;
}

export async function runAll() {
  await runGo([]);
  await runDocs([]);
  await runScripts([]);
}

export function printUsage() {
  console.log(`Usage: tests <subcommand> [flags]

Subcommands:
  go [flags] [./path/...]    Run Go tests (gotestsum)
  docs [args]                Run docs tests (pnpm --filter docs)
  scripts [args]             Run script tests (pnpm --filter scripts)
  e2e [args]                 Run e2e tests (chromedp Go suite)
  all                        Run go + docs + scripts (not e2e)`);
  process.exitCode = 1;
}

if (import.meta.filename === process.argv[1]) {
  const subcommand = process.argv[2];
  const rest = process.argv.slice(3);

  switch (subcommand) {
    case "go":
      await runGo(rest);
      break;
    case "docs":
      await runDocs(rest);
      break;
    case "scripts":
      await runScripts(rest);
      break;
    case "e2e":
      await runE2e(rest);
      break;
    case "all":
      await runAll();
      break;
    default:
      printUsage();
      break;
  }
}
