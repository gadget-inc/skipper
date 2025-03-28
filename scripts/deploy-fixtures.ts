#!/usr/bin/env -S deno run -A
import { currentImageDigest, currentImageTag, deployKraneNamespace, isCI } from "./_utils.ts";

const enableOtel = !isCI;

if (!isCI) {
  await deployKraneNamespace("skipper-development-fixtures", {
    echo_image_tag: await currentImageTag(),
    echo_image_digest: await currentImageDigest("skipper-fixtures-echo"),
    extra_env: {
      OTEL_DENO: enableOtel,
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
    },
  });
}

await deployKraneNamespace("skipper-test-fixtures", {
  echo_image_tag: await currentImageTag(),
  echo_image_digest: !isCI ? await currentImageDigest("skipper-fixtures-echo") : undefined,
  extra_env: {
    OTEL_DENO: enableOtel,
    OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
    OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
  },
});
