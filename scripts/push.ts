#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";
import { gitSha } from "./_utils.ts";

const sha = await gitSha();
await $`docker tag fusion:${sha} us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:${sha}`;
await $`docker push us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:${sha}`;
