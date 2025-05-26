#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

const result = await $`deno fmt`;
Deno.exit(result.exitCode ?? 1);
