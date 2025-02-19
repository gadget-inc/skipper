#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, isCI } from "./_utils.ts";

if (!isCI) {
  await deployKraneNamespace("fusion-fixtures-development");
}

await deployKraneNamespace("fusion-fixtures-test");
