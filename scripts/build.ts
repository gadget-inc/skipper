#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import process from "node:process";
import { parseArgs } from "node:util";

import { dedent } from "ts-dedent";
import { $, path } from "zx";

import { abs, currentDockerPlatform, currentImageTag, expandSkipperAlias, isCI } from "./_utils.ts";

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
    only: { type: "string", default: "controller,router,fixtures" },
    provenance: { type: "boolean", default: false },
    push: { type: "boolean", default: false },
  },
});

// Expand "skipper" to "controller,router" for convenience
flags.values.only = expandSkipperAlias(flags.values.only);

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

const components = new Set(flags.values.only.split(","));
const buildController = components.has("controller");
const buildRouter = components.has("router");

if (buildController || buildRouter) {
  const targets: string[] = [];
  if (buildController) targets.push("controller");
  if (buildRouter) targets.push("router");

  const bakeFlags: string[] = [];

  if (flags.values.load) {
    bakeFlags.push("--load");
  }

  if (flags.values.push) {
    bakeFlags.push("--push");
  }

  if (!flags.values.provenance) {
    bakeFlags.push("--provenance=false");
  }

  // Set bake variables via environment
  const bakeEnv = {
    ...process.env,
    TAG: flags.values.tag,
    PLATFORM: flags.values.platform,
    ...(flags.values.registry && { REGISTRY: flags.values.registry }),
  };

  await $({ verbose: true, env: bakeEnv })`docker buildx bake ${targets} ${bakeFlags}`;

  if (flags.values.kind) {
    const controllerImageName = flags.values.registry ? `${flags.values.registry}/skipper-controller` : "skipper-controller";
    const routerImageName = flags.values.registry ? `${flags.values.registry}/skipper-router` : "skipper-router";

    if (buildController) {
      await $`kind load docker-image ${controllerImageName}:${flags.values.tag}`;
    }
    if (buildRouter) {
      await $`kind load docker-image ${routerImageName}:${flags.values.tag}`;
    }
  }
}

if (components.has("fixtures")) {
  const fixture = abs("fixture");
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
