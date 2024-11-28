import { $, cd, glob, path } from "npm:zx";
import { abs, amd64DockerImageTag, arm64DockerImageTag } from "./_utils.ts";

const arm64Tag = await arm64DockerImageTag();
const amd64Tag = await amd64DockerImageTag();

for (const fixture of await glob(abs("docker/fixtures/*"), { onlyDirectories: true })) {
  cd(fixture);
  const name = path.basename(fixture);
  await $`docker buildx build . --tag=fusion-fixture-${name}:${amd64Tag} --platform=linux/amd64`;
  await $`docker buildx build . --tag=fusion-fixture-${name}:${arm64Tag} --platform=linux/arm64/v8`;
}
