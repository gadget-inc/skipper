#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import { mkdir } from "node:fs/promises";
import process from "node:process";
import { $ } from "zx";
import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await mkdir(abs("tmp/logs"), { recursive: true });

const goTestFlags = process.argv.slice(2);
if (!goTestFlags.some((arg) => arg.startsWith("./"))) {
  // run all tests if a path wasn't provided
  goTestFlags.unshift("./...");
}

await $nothrow`go test ${goTestFlags} | tee tmp/logs/tests.log`;
