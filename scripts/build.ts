#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

$.cwd = abs();

const flags = parseArgs(Deno.args, {
  string: ["tag", "platform", "cache-from-to"],
  boolean: ["load", "push", "kind"],
  negatable: ["kind"],
  default: {
    image: "fusion",
    tag: await defaultImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
    "cache-from-to": isCI ? "gha" : "",
    load: true,
    push: false,
  },
});

let load = "";
if (flags.load) {
  load = `--load`;
}

let push = "";
if (flags.push) {
  push = `--push`;
}

let cacheFromTo = "";
switch (flags["cache-from-to"]) {
  case "gha":
    cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
    break;
  case "registry":
    cacheFromTo = `--cache-from=type=registry,ref=${flags.image}:buildcache --cache-to=type=registry,ref=${flags.image}:buildcache,mode=max`;
    break;
}

await $({
  verbose: true,
})`docker buildx build . --tag=${flags.image}:${flags.tag} --platform=${flags.platform} ${cacheFromTo} ${load} ${push}`;

if (flags.kind) {
  await $`kind load docker-image ${flags.image}:${flags.tag}`;
}
