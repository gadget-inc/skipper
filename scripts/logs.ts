#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import process from "node:process";
import { parseArgs } from "node:util";
import { $ } from "zx";
import { abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";
$.env["SKIPPER_KUBECTL_CONTEXT"] ??= "orbstack";

const flags = parseArgs({
  args: process.argv.slice(2),
  options: {
    namespace: { type: "string", default: "skipper-development,skipper-development-fixtures,skipper-test,skipper-test-fixtures" },
    file: { type: "string", default: abs("tmp/logs/logs.log"), short: "f" },
  },
});

await mkdir(dirname(flags.values.file), { recursive: true });

await $`stern . --context="$SKIPPER_KUBECTL_CONTEXT" --namespace=${flags.values.namespace} --template='{{ printf "%s/%s: %s\\n" .Namespace .PodName .Message }}' | tee ${flags.values.file}`;
