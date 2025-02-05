#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, gitSha } from "./_utils.ts";

$.cwd = abs();

const sha = await gitSha();
await $`docker buildx build . --tag=fusion:${sha} --platform=linux/amd64,linux/arm64 --load`;
if (Deno.env.has("CI")) {
  await $`kind load docker-image fusion:${sha}`;
}
