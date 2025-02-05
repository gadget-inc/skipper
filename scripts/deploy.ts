#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, gitSha, isCI } from "./_utils.ts";

const sha = await gitSha();

if (!isCI) {
  await deployKraneNamespace("fusion-development", { imageTag: sha });
}

await deployKraneNamespace("fusion-test", { imageTag: sha });
