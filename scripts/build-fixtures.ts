#!/usr/bin/env -S deno run -A
import { $, glob, minimist, path } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";

const { tag = await defaultImageTag(), platform = await currentDockerPlatform() } = minimist(Deno.args);

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);
  await $`docker buildx build . --load --tag=fusion-fixtures-${name}:${tag} --platform=${platform} ${cacheFromTo}`;
  if (isCI) {
    await $`kind load docker-image fusion-fixtures-${name}:${tag}`;
  }
}
