import { arm64DockerImageTag, deployKraneNamespace } from "./_utils.ts";

const imageTag = await arm64DockerImageTag();

await deployKraneNamespace("fusion-fixtures-development", { imageTag });
await deployKraneNamespace("fusion-fixtures-test", { imageTag });
