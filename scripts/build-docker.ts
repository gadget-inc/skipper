import { $, cd } from "npm:zx";
import { abs } from "./_utils.ts";

// const os = Deno.build.os;
// const arch = Deno.build.arch === "x86_64" ? "amd64" : "arm64";
// const suffix = arch === "amd64" ? "v1" : "v8.0";

await Deno.copyFile(
    abs(`dist/fusion_linux_arm64_v8.0/fusion`),
    abs("docker/fusion/fusion"),
);
cd(abs("docker/fusion"));
await $`docker buildx build --tag=fusion:latest --platform=linux/arm64 .`;

cd(abs("docker/example-deno"));
await $`docker buildx build --tag=fusion-example-deno:latest --platform=linux/arm64 .`;
