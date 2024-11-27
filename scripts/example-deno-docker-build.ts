import { $, cd } from "npm:zx";
import { abs, amd64DockerImageTag, arm64DockerImageTag } from "./_utils.ts";

const arm64Tag = await arm64DockerImageTag();
const amd64Tag = await amd64DockerImageTag();

cd(abs("docker/example-deno"));
await $`docker buildx build . --tag=fusion-example-deno:${amd64Tag} --platform=linux/amd64`;
await $`docker buildx build . --tag=fusion-example-deno:${arm64Tag} --platform=linux/arm64/v8`;
