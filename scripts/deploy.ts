import { $ } from "npm:zx";
import { arm64DockerImageTag, deployKraneNamespace, renderKraneNamespace } from "./_utils.ts";

const clusterRenderDir = await renderKraneNamespace("cluster");
await $`krane global-deploy orbstack -f ${clusterRenderDir}/* --selector=app.kubernetes.io/managed-by=krane `;

const kubeSystemRenderDir = await renderKraneNamespace("kube-system");
await $`krane deploy kube-system orbstack -f ${kubeSystemRenderDir}/* --selector=app.kubernetes.io/managed-by=krane --protected-namespaces=default kube-public`;

await deployKraneNamespace("example-development", { imageTag: await arm64DockerImageTag() });
await deployKraneNamespace("fusion-development", { imageTag: await arm64DockerImageTag() });
