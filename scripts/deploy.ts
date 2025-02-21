#!/usr/bin/env -S deno run -A
import { abs, deployKraneNamespace, isCI } from "./_utils.ts";

const enableOtel = !isCI;

if (!isCI) {
  await deployKraneNamespace("fusion-development", {
    namespace: "fusion-development",
    function_namespaces: ["fusion-fixtures-development"],
    unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
    router_node_port: 31020,
    extra_env: {
      FUSION_TELEMETRY: enableOtel,
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
    },
  });
}

await deployKraneNamespace("fusion-test", {
  namespace: "fusion-test",
  function_namespaces: ["fusion-fixtures-test"],
  unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
  router_node_port: 31021,
  extra_env: {
    FUSION_TELEMETRY: enableOtel,
    OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
    OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
  },
});
