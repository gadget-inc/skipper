#!/usr/bin/env -S deno run -A
import { minimist } from "npm:zx";
import { defaultImageTag, deployKraneNamespace, isCI } from "./_utils.ts";

const { tag = await defaultImageTag() } = minimist(Deno.args);

if (!isCI) {
  await deployKraneNamespace("fusion-fixtures-development", { image_tag: tag });
}

await deployKraneNamespace("fusion-fixtures-test", { image_tag: tag });
