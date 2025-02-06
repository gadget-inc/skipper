#!/usr/bin/env -S deno run -A
import { defaultImageTag, deployKraneNamespace, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag"],
  default: {
    tag: await defaultImageTag(),
  },
});

if (!isCI) {
  await deployKraneNamespace("fusion-development", { image_tag: flags.tag });
}

await deployKraneNamespace("fusion-test", { image_tag: flags.tag });
