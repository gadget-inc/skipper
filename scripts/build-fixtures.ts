#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, gitSha } from "./_utils.ts";

const sha = await gitSha();

for (const fixture of await glob(abs("docker/fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);
  await $`docker buildx build . --tag=fusion-fixture-${name}:${sha} --platform=linux/amd64,linux/arm64 --load`;
  if (Deno.env.has("CI")) {
    await $`kind load docker-image fusion-fixture-${name}:${sha}`;
  }
}
