#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs } from "./_utils.ts";
import { ensureDir } from "jsr:@std/fs";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await ensureDir(abs("tmp/logs"));

if (!Deno.args.some((arg) => arg.startsWith("./"))) {
  // run all tests if a path wasn't provided
  Deno.args.unshift("./...");
}

const result = await $`go test ${Deno.args} | tee -a tmp/logs/tests.log`.nothrow();
Deno.exit(result.exitCode ?? 1);
