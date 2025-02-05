#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, gitSha } from "./_utils.ts";

const sha = await gitSha();
$.cwd = abs();

await $`docker buildx build . --tag=fusion:${sha} --load`;
if (Deno.env.has("CI")) {
  await $`kind load docker-image fusion:${sha}`;
}
