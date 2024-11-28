import { arm64DockerImageTag, deployKraneNamespace } from "./_utils.ts";

const imageTag = await arm64DockerImageTag();

await deployKraneNamespace("fusion-development", { imageTag });
await deployKraneNamespace("fusion-test", { imageTag });
