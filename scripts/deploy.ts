import { $, cd } from "npm:zx";
import { workDir } from "./_utils.ts";

const CONTEXT = "orbstack";

cd(workDir);

// await $`kubectl --context=${CONTEXT} --namespace=fusion-development apply -f k8s/fusion.yaml`;
await $`kubectl --context=${CONTEXT} --namespace=example apply -f k8s/example.yaml`;
