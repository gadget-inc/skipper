#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, gitSha, isCI } from "./_utils.ts";

const sha = await gitSha();
$.cwd = abs();

await $`docker buildx build . --tag=fusion:${sha} --platform=linux/amd64,linux/arm64 --load`;
if (isCI) {
  await $`kind load docker-image fusion:${sha}`;
}
