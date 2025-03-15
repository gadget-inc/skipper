#!/usr/bin/env -S deno run -A
import { abs, deployKraneNamespace, isCI } from "./_utils.ts";

const enableOtel = !isCI;

if (!isCI) {
  await deployKraneNamespace("skipper-development", {
    namespace: "skipper-development",
    function_namespaces: ["skipper-development-fixtures"],
    unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
    router_node_port: 31020,
    env: {
      SKIPPER_TELEMETRY: enableOtel,
      SKIPPER_TELEMETRY_METRIC_OTLP: enableOtel,
      OTEL_METRIC_EXPORT_INTERVAL: "1s",
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
    },
    annotations: {
      "shipit.shopify.io/restart": "true",
    },
  });
}

await deployKraneNamespace("skipper-test", {
  namespace: "skipper-test",
  function_namespaces: ["skipper-test-fixtures"],
  unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
  router_node_port: 31021,
  env: {
    SKIPPER_TELEMETRY: enableOtel,
    SKIPPER_TELEMETRY_METRIC_OTLP: enableOtel,
    OTEL_METRIC_EXPORT_INTERVAL: "1s",
    OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
    OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
  },
  annotations: {
    "shipit.shopify.io/restart": "true",
  },
});
