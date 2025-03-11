#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, isCI } from "./_utils.ts";

const enableOtel = !isCI;

if (!isCI) {
  await deployKraneNamespace("skipper-development-fixtures", {
    extra_env: {
      OTEL_DENO: enableOtel,
      OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
      OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
    },
  });
}

await deployKraneNamespace("skipper-test-fixtures", {
  extra_env: {
    OTEL_DENO: enableOtel,
    OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
    OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
  },
});
