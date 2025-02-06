#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { minimist } from "npm:zx";

const { tag = await defaultImageTag(), platform = await currentDockerPlatform(), kind = isCI } = minimist(Deno.args, { boolean: ["kind"] });

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

$.cwd = abs();

await $`docker buildx build . --load --tag=fusion:${tag} --platform=${platform} ${cacheFromTo}`;

if (kind) {
  await $`kind load docker-image fusion:${tag}`;
}
