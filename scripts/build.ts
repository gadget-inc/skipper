#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, gitSha, isCI } from "./_utils.ts";
import { minimist } from "npm:zx";

const { platform = await currentDockerPlatform() } = minimist(Deno.args);

const sha = await gitSha();

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

$.cwd = abs();
await $`docker buildx build . --load --tag=fusion:sha-${sha} --platform=${platform} ${cacheFromTo}`;
if (isCI) {
  await $`kind load docker-image fusion:${sha}`;
}
