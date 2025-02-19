#!/usr/bin/env -S deno run -A
import { abs, deployKraneNamespace, isCI } from "./_utils.ts";

if (!isCI) {
  await deployKraneNamespace("fusion-development", {
    namespace: "fusion-development",
    function_namespaces: ["fusion-fixtures-development"],
    unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
    router_node_port: 31020,
  });
}

await deployKraneNamespace("fusion-test", {
  namespace: "fusion-test",
  function_namespaces: ["fusion-fixtures-test"],
  unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
  router_node_port: 31021,
});
