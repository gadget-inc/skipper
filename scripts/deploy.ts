import { arm64DockerImageTag, deployKraneNamespace } from "./_utils.ts";

await deployKraneNamespace("fusion-development", { imageTag: await arm64DockerImageTag() });
