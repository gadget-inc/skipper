#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { abs, currentDockerPlatform, defaultImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

$.cwd = abs();

const flags = parseArgs(Deno.args, {
  string: ["repo", "tag", "platform", "cache-from-to"],
  boolean: ["latest", "load", "push", "kind", "provenance"],
  negatable: ["kind"],
  default: {
    repo: "skipper",
    tag: await defaultImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
    "cache-from-to": isCI ? "gha" : "",
    latest: true,
    load: true,
    push: false,
    provenance: false,
  },
});

const buildFlags = [`--platform=${flags.platform}`, `--tag=${flags.repo}:${flags.tag}`];

if (flags.latest) {
  buildFlags.push(`--tag=${flags.repo}:latest`);
}

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
    buildFlags.push(`--cache-from=type=registry,ref=${flags.repo}:buildcache`);
    buildFlags.push(`--cache-to=type=registry,ref=${flags.repo}:buildcache,mode=max`);
    break;
}

if (!flags.provenance) {
  buildFlags.push("--provenance=false");
}

await $({ verbose: true })`docker buildx build . ${buildFlags}`;

if (flags.kind) {
  await $`kind load docker-image ${flags.repo}:${flags.tag}`;
  if (flags.latest) {
    await $`kind load docker-image ${flags.repo}:latest`;
  }
}
