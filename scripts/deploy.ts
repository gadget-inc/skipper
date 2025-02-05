#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, gitSha } from "./_utils.ts";

const sha = await gitSha();
await deployKraneNamespace("fusion-development", { imageTag: sha });
await deployKraneNamespace("fusion-test", { imageTag: sha });
