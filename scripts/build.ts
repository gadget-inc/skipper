#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag", "platform"],
  boolean: ["kind"],
  negatable: ["kind"],
  default: {
    tag: await defaultImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
  },
});

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

$.cwd = abs();

await $`docker buildx build . --load --tag=fusion:${flags.tag} --platform=${flags.platform} ${cacheFromTo}`;

if (flags.kind) {
  await $`kind load docker-image fusion:${flags.tag}`;
}
