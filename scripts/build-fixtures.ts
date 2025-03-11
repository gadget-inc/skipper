#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, currentDockerPlatform, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["platform"],
  boolean: ["kind"],
  negatable: ["kind"],
  default: {
    platform: await currentDockerPlatform(),
    kind: isCI,
  },
});

let cacheFromTo = "";
if (isCI) {
  cacheFromTo = "--cache-from=type=gha --cache-to=type=gha,mode=max";
}

for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
  $.cwd = fixture;
  const name = path.basename(fixture);

  await $`docker buildx build . --load --tag=skipper-fixtures-${name}:latest --platform=${flags.platform} ${cacheFromTo}`;

  if (flags.kind) {
    await $`kind load docker-image skipper-fixtures-${name}:latest`;
  }
}
