#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

const flags = parseArgs(Deno.args, {
  string: ["only"],
  default: {
    only: "go,deno",
  },
});

let exitCode = 0;
function setExitCode(code: number | null) {
  if (code === 0) {
    return;
  }

  exitCode = code ?? 1;
}

if (flags.only.includes("go")) {
  const result = await $`golangci-lint run`.nothrow();
  setExitCode(result.exitCode);
  console.log();
}

if (flags.only.includes("deno")) {
  let result = await $`deno lint`.nothrow();
  setExitCode(result.exitCode);
  console.log();

  result = await $`deno fmt --check`.nothrow();
  setExitCode(result.exitCode);
  console.log();
}

Deno.exit(exitCode);
