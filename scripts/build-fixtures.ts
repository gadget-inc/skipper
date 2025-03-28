#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, currentDockerPlatform, currentImageTag, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag", "platform"],
  boolean: ["latest", "load", "kind", "provenance"],
  negatable: ["kind"],
  default: {
    tag: await currentImageTag(),
    platform: await currentDockerPlatform(),
    kind: isCI,
    latest: true,
    load: true,
    provenance: false,
  },
});

for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);
  const buildFlags = [`--platform=${flags.platform}`, `--tag=skipper-fixtures-${name}:${flags.tag}`];

  if (flags.latest) {
    buildFlags.push(`--tag=skipper-fixtures-${name}:latest`);
  }

  if (flags.load) {
    buildFlags.push("--load");
  }

  if (!flags.provenance) {
    buildFlags.push("--provenance=false");
  }

  if (isCI) {
    buildFlags.push("--cache-from=type=gha");
    buildFlags.push("--cache-to=type=gha,mode=max");
  }

  await $({ verbose: true })`docker buildx build . ${buildFlags}`;

  if (flags.kind) {
    await $`kind load docker-image skipper-fixtures-${name}:${flags.tag}`;
    if (flags.latest) {
      await $`kind load docker-image skipper-fixtures-${name}:latest`;
    }
  }
}
