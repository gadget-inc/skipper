import { $ } from "npm:zx";
import { renderKraneNamespace } from "./_utils.ts";

$.env.KUBECTL_CONTEXT ??= "orbstack";

const clusterRenderDir = await renderKraneNamespace("cluster");
await $`krane global-deploy "$KUBECTL_CONTEXT" -f ${clusterRenderDir}/* --selector=app.kubernetes.io/managed-by=krane `;

const kubeSystemRenderDir = await renderKraneNamespace("kube-system");
await $`krane deploy kube-system "$KUBECTL_CONTEXT" -f ${kubeSystemRenderDir}/* --selector=app.kubernetes.io/managed-by=krane --protected-namespaces=default kube-public`;
