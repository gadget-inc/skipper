#!/usr/bin/env -S deno run -A
import { parseArgs } from "jsr:@std/cli/parse-args";
import { defaultImageTag, deployKraneNamespace, isCI } from "./_utils.ts";

const flags = parseArgs(Deno.args, {
  string: ["tag"],
  default: {
    tag: await defaultImageTag(),
  },
});

if (!isCI) {
  await deployKraneNamespace("fusion-fixtures-development", { image_tag: flags.tag });
}

await deployKraneNamespace("fusion-fixtures-test", { image_tag: flags.tag });
