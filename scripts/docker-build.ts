import { $, cd } from "npm:zx";
import { abs, amd64DockerImageTag, arm64DockerImageTag } from "./_utils.ts";

const arm64Tag = await arm64DockerImageTag();
// const amd64Tag = await amd64DockerImageTag();

cd(abs("docker/fusion"));
// await Deno.copyFile(abs(`dist/fusion_linux_amd64_v1/fusion`), abs("docker/fusion/fusion"));
// await $`docker buildx build . --tag=fusion:${amd64Tag} --platform=linux/amd64`;

await Deno.copyFile(abs(`dist/fusion_linux_arm64_v8.0/fusion`), abs("docker/fusion/fusion"));
await $`docker buildx build . --tag=fusion:${arm64Tag} --platform=linux/arm64/v8`;

// cd(abs("docker/example-deno"));
// await $`docker buildx build . --tag=fusion-example-deno:${amd64Tag} --platform=linux/amd64`;
// await $`docker buildx build . --tag=fusion-example-deno:${arm64Tag} --platform=linux/arm64/v8`;
