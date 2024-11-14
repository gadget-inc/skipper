import { $ } from "npm:zx";
import { deployKraneNamespace } from "./_utils.ts";
import { renderKraneNamespace } from "./_utils.ts";

const clusterRenderDir = await renderKraneNamespace("cluster");
await $`krane global-deploy orbstack -f ${clusterRenderDir}/* --selector=app.kubernetes.io/managed-by=krane `;

const kubeSystemRenderDir = await renderKraneNamespace("kube-system");
await $`krane deploy kube-system orbstack -f ${kubeSystemRenderDir}/* --selector=app.kubernetes.io/managed-by=krane --protected-namespaces=default kube-public`;

await deployKraneNamespace("example-development");
await deployKraneNamespace("fusion-development");
