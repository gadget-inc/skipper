#!/usr/bin/env -S deno run -A
import { $, path } from "npm:zx";
import { abs } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";
import { ensureDir } from "jsr:@std/fs";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";
$.env.SKIPPER_KUBECTL_CONTEXT ??= "orbstack";

const flags = parseArgs(Deno.args, {
  string: ["namespace", "file"],
  default: {
    namespace: "skipper-development,skipper-development-fixtures,skipper-test,skipper-test-fixtures",
    file: abs("tmp/logs/logs.log"),
  },
  alias: {
    f: "file",
  },
});

await ensureDir(path.dirname(flags.file));

await $`stern . --context="$SKIPPER_KUBECTL_CONTEXT" --namespace=${flags.namespace} --template='{{ printf "%s/%s: %s\\n" .Namespace .PodName .Message }}' | tee ${flags.file}`;
