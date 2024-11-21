import { $ } from "npm:zx";
import { amd64DockerImageTag } from "./_utils.ts";

const amd64Tag = await amd64DockerImageTag();
await $`docker tag fusion:${amd64Tag} us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:${amd64Tag}`;
await $`docker push us-central1-docker.pkg.dev/gadget-core-production/core-production/fusion:${amd64Tag}`;
