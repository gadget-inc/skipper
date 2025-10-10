#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import process from "node:process";
import { parseArgs } from "node:util";
import { dedent } from "ts-dedent";
import { $, glob, path } from "zx";
import { abs, currentDockerPlatform, currentImageTag, isCI } from "./_utils.ts";

$.cwd = abs();

const flags = parseArgs({
  args: process.argv.slice(2),
  allowNegative: true,
  options: {
    registry: { type: "string" },
    tag: { type: "string", default: await currentImageTag() },
    platform: { type: "string", default: await currentDockerPlatform() },
    help: { type: "boolean", default: false },
    kind: { type: "boolean", default: isCI },
    load: { type: "boolean", default: true },
    only: { type: "string", default: "skipper,fixtures" },
    provenance: { type: "boolean", default: false },
    push: { type: "boolean", default: false },
  },
});

if (flags.values.help) {
  console.log(dedent`
    Usage:
      build [flags]

    Flags:
          --load                    Load the image (${flags.values.load})
          --only <string>           Only build the given images (${flags.values.only})
          --platform <string>       Platform to build for (${flags.values.platform})
          --provenance              Enable provenance (${flags.values.provenance})
          --push                    Push the image (${flags.values.push})
          --registry <string>       Registry of the image (${flags.values.registry})
          --tag <string>            Build tag (${flags.values.tag})
      -h, --help                    Show this help message
  `);
  process.exit(0);
}

if (flags.values.only.includes("skipper")) {
  const imageName = flags.values.registry ? `${flags.values.registry}/skipper` : "skipper";
  const buildFlags = [`--platform=${flags.values.platform}`, `--tag=${imageName}:${flags.values.tag}`];

  if (flags.values.load) {
    buildFlags.push("--load");
  }

  if (flags.values.push) {
    buildFlags.push("--push");
  }

  if (!flags.values.provenance) {
    buildFlags.push("--provenance=false");
  }

  await $({ verbose: true })`docker buildx build . ${buildFlags}`;

  if (flags.values.kind) {
    await $`kind load docker-image ${imageName}:${flags.values.tag}`;
  }
}

if (flags.values.only.includes("fixtures")) {
  for (const fixture of await glob(abs("fixtures/*"), { onlyDirectories: true })) {
    const name = path.basename(fixture);
    const imageName = `skipper-fixtures-${name}`;
    const buildFlags = [
      `--file=${fixture}/Dockerfile`,
      `--platform=${flags.values.platform}`,
      `--tag=${imageName}:${flags.values.tag}`,
      `--build-arg=NODE_VERSION=${process.version.slice(1)}`,
    ];

    if (flags.values.load) {
      buildFlags.push("--load");
    }

    if (!flags.values.provenance) {
      buildFlags.push("--provenance=false");
    }

    await $({ verbose: true })`docker buildx build . ${buildFlags}`;

    if (flags.values.kind) {
      await $`kind load docker-image ${imageName}:${flags.values.tag}`;
    }
  }
}
