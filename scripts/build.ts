#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag", "platform", "cache-from-to"],
  boolean: ["kind"],
  negatable: ["kind"],
  default: {
    tag: await defaultImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
    "cache-from-to": isCI ? "gha" : "",
  },
});

let cacheFromTo = "";
switch (flags["cache-from-to"]) {
  case "gha":
    cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
    break;
  case "registry":
    cacheFromTo =
      "--cache-from=type=registry,ref=us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:buildcache --cache-to=type=registry,ref=us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:buildcache,mode=max";
    break;
}

$.cwd = abs();

await $`docker buildx build . --load --tag=fusion:${flags.tag} --platform=${flags.platform} ${cacheFromTo}`;

if (flags.kind) {
  await $`kind load docker-image fusion:${flags.tag}`;
}
