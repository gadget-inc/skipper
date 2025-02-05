#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, gitSha, isCI } from "./_utils.ts";

const sha = await gitSha();

for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);
  await $`docker buildx build . --tag=fusion-fixture-${name}:${sha} --platform=linux/amd64,linux/arm64 --load`;
  if (isCI) {
    await $`kind load docker-image fusion-fixture-${name}:${sha}`;
  }
}
