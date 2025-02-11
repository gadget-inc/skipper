#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag", "platform"],
  boolean: ["kind", "cache-to"],
  negatable: ["kind", "cache-to"],
  default: {
    tag: await defaultImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
    "cache-to": false,
  },
});

let cacheFromTo = "--cache-from=type=registry,ref=us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:buildcache";
if (flags["cache-to"]) {
  cacheFromTo += " --cache-to=type=registry,ref=us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:buildcache";
}

$.cwd = abs();

await $`docker buildx build . --load --tag=fusion:${flags.tag} --platform=${flags.platform} ${cacheFromTo}`;

if (flags.kind) {
  await $`kind load docker-image fusion:${flags.tag}`;
}
