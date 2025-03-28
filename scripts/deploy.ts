#!/usr/bin/env -S deno run -A
import { abs, currentImageDigest, currentImageTag, deployKraneNamespace, isCI } from "./_utils.ts";

const enableOtel = !isCI;

if (!isCI) {
  await deployKraneNamespace("skipper-development", {
    image_tag: await currentImageTag(),
    image_digest: await currentImageDigest("skipper"),
    namespace: "skipper-development",
    function_namespaces: ["skipper-development-fixtures"],
    unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
    router_node_port: 31020,
    controller_node_port: 31021,
    env: {
      SKIPPER_TELEMETRY: enableOtel,
      SKIPPER_TELEMETRY_METRIC_OTLP: enableOtel,
      OTEL_METRIC_EXPORT_INTERVAL: "1000",
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
    },
    annotations: {
      "shipit.shopify.io/restart": "true",
    },
  });
}

await deployKraneNamespace("skipper-test", {
  image_tag: await currentImageTag(),
  image_digest: !isCI ? await currentImageDigest("skipper") : undefined,
  namespace: "skipper-test",
  function_namespaces: ["skipper-test-fixtures"],
  unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
  router_node_port: 31030,
  controller_node_port: 31031,
  env: {
    SKIPPER_TELEMETRY: enableOtel,
    SKIPPER_TELEMETRY_METRIC_OTLP: enableOtel,
    OTEL_METRIC_EXPORT_INTERVAL: "1000",
    OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
    OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
  },
  annotations: {
    "shipit.shopify.io/restart": "true",
  },
});
