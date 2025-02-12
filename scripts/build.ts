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

const buildFlags = [`--tag=${flags.image}:${flags.tag}`, `--platform=${flags.platform}`];

if (flags.load) {
  buildFlags.push("--load");
}

if (flags.push) {
  buildFlags.push("--push");
}

switch (flags["cache-from-to"]) {
  case "gha":
    buildFlags.push("--cache-from=type=gha");
    buildFlags.push("--cache-to=type=gha,mode=max");
    break;
  case "registry":
    buildFlags.push(`--cache-from=type=registry,ref=${flags.image}:buildcache`);
    buildFlags.push(`--cache-to=type=registry,ref=${flags.image}:buildcache,mode=max`);
    break;
}

await $({ verbose: true })`docker buildx build . ${buildFlags}`;

if (flags.kind) {
  await $`kind load docker-image ${flags.image}:${flags.tag}`;
}
