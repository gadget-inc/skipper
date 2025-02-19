#!/usr/bin/env -S deno run -A
import { abs, defaultImageTag, deployKraneNamespace, isCI } from "./_utils.ts";
import { parseArgs } from "jsr:@std/cli/parse-args";

const flags = parseArgs(Deno.args, {
  string: ["tag"],
  default: {
    tag: await defaultImageTag(),
  },
});

if (!isCI) {
  await deployKraneNamespace("fusion-development", {
    namespace: "fusion-development",
    image_repository: "fusion",
    image_tag: flags.tag,
    image_pull_policy: "Never",
    function_namespaces: ["fusion-fixtures-development"],
    unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
    router_node_port: 31020,
  });
}

await deployKraneNamespace("fusion-test", {
  namespace: "fusion-test",
  image_repository: "fusion",
  image_tag: flags.tag,
  image_pull_policy: "Never",
  function_namespaces: ["fusion-fixtures-test"],
  unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
  router_node_port: 31021,
});
