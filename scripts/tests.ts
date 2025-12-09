#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { mkdir } from "node:fs/promises";
import process from "node:process";
import { $ } from "zx";
import { $nothrow, abs, isCI } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await mkdir(abs("tmp/logs"), { recursive: true });

const goTestFlags = process.argv.slice(2);

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
