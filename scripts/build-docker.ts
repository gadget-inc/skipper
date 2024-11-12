import { $, cd } from "npm:zx";
import { absolute } from "./_utils.ts";

// const os = Deno.build.os;
// const arch = Deno.build.arch === "x86_64" ? "amd64" : "arm64";
// const suffix = arch === "amd64" ? "v1" : "v8.0";

await Deno.copyFile(
    absolute(`dist/fusion_linux_arm64_v8.0/fusion`),
    absolute("docker/fusion/fusion"),
);
cd(absolute("docker/fusion"));
await $`docker buildx build --tag=fusion:latest --platform=linux/arm64 .`;

cd(absolute("docker/example-deno"));
await $`docker buildx build --tag=fusion-example:latest --platform=linux/arm64 .`;
