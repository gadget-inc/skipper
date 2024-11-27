import { arm64DockerImageTag, deployKraneNamespace } from "./_utils.ts";

await deployKraneNamespace("example-development", { imageTag: await arm64DockerImageTag() });
