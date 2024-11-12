import { which } from "npm:zx";
import { deployKraneNamespace } from "./_utils.ts";

await which("krane");
await deployKraneNamespace("example-development");
// await deployKraneNamespace("fusion-development");
