#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, gitSha, isCI } from "./_utils.ts";

const sha = await gitSha();

const platforms = ["linux/amd64"];
if (!isCI) {
  platforms.push("linux/arm64");
}

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);
  await $`docker buildx build . --tag=fusion-fixture-${name}:${sha} --platform=${platforms.join(",")} --load ${cacheFromTo}`;
  if (isCI) {
    await $`kind load docker-image fusion-fixture-${name}:${sha}`;
  }
}
