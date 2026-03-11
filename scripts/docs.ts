#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import process from "node:process";
import { $ } from "zx";
import { abs } from "./_utils.ts";

$.cwd = abs("docs");
$.verbose = true;
$.stdio = "inherit";

const command = process.argv[2] ?? "dev";
const args = process.argv.slice(3);

switch (command) {
  case "dev":
    await $`pnpm astro dev ${args}`;
    break;
  case "build":
    await $`pnpm astro build ${args}`;
    break;
  case "preview":
    await $`pnpm astro preview ${args}`;
    break;
  default:
    console.error(`Unknown command: ${command}. Available: dev, build, preview`);
    process.exit(1);
}
