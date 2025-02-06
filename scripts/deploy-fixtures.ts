#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, gitSha, isCI } from "./_utils.ts";

const sha = await gitSha();

if (!isCI) {
  await deployKraneNamespace("fusion-fixtures-development", { imageTag: `sha-${sha}` });
}

await deployKraneNamespace("fusion-fixtures-test", { imageTag: `sha-${sha}` });
