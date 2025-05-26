#!/usr/bin/env -S deno run -A
import { $, glob, path } from "npm:zx";
import { abs, currentDockerPlatform, currentImageTag, isCI } from "./_utils.ts";
import { dedent } from "npm:ts-dedent";
import { parseArgs } from "jsr:@std/cli/parse-args";

$.cwd = abs();

const flags = parseArgs(Deno.args, {
  string: ["registry", "name", "tag", "platform", "cache-from-to"],
  boolean: ["help", "latest", "load", "push", "kind", "provenance", "fixtures"],
  negatable: ["kind", "fixtures"],
  default: {
    "cache-from-to": isCI ? "gha" : "",
    fixtures: true,
    help: false,
    kind: isCI,
    latest: true,
    load: true,
    name: "skipper",
    platform: await currentDockerPlatform(),
    provenance: false,
    push: false,
    registry: "",
    tag: await currentImageTag(),
  },
  alias: {
    h: "help",
  },
});

if (flags.help) {
  console.log(dedent`
    Usage:
      build [flags]

    Flags:
          --cache-from-to <string>  Build cache to use (${flags["cache-from-to"]})
          --fixtures                Build fixtures (${flags.fixtures})
          --latest                  Build latest tag (${flags.latest})
          --load                    Load the image (${flags.load})
          --name <string>           Name of the image (${flags.name})
          --platform <string>       Platform to build for (${flags.platform})
          --provenance              Enable provenance (${flags.provenance})
          --push                    Push the image (${flags.push})
          --registry <string>       Registry of the image (${flags.registry})
          --tag <string>            Build tag (${flags.tag})
      -h, --help                    Show this help message
  `);
  Deno.exit(0);
}

if (flags.only.includes("skipper")) {
  const imageName = flags.registry ? `${flags.registry}/${flags.name}` : flags.name;
  const buildFlags = [`--platform=${flags.platform}`, `--tag=${imageName}:${flags.tag}`];

  if (flags.latest) {
    buildFlags.push(`--tag=${imageName}:latest`);
  }

  if (flags.load) {
    buildFlags.push("--load");
  }

  if (flags.push) {
    buildFlags.push("--push");
  }

  if (!flags.provenance) {
    buildFlags.push("--provenance=false");
  }

  await $`docker buildx build . ${buildFlags}`;

  if (flags.kind) {
    await $`kind load docker-image ${imageName}:${flags.tag}`;
    if (flags.latest) {
      await $`kind load docker-image ${imageName}:latest`;
    }
  }
}

if (flags.fixtures) {
  for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
    $.cwd = fixture;
    const name = path.basename(fixture);
    const imageName = `skipper-fixtures-${name}`;
    const buildFlags = [`--platform=${flags.platform}`, `--tag=${imageName}:${flags.tag}`];

    if (flags.latest) {
      buildFlags.push(`--tag=${imageName}:latest`);
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
      await $`kind load docker-image ${imageName}:${flags.tag}`;
      if (flags.latest) {
        await $`kind load docker-image ${imageName}:latest`;
      }
    }
  }
}
