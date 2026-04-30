#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { generateKeyPairSync } from "node:crypto";
import { existsSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import process from "node:process";
import { parseArgs } from "node:util";

import { dedent } from "ts-dedent";
import { $ } from "zx";

import { $stdout, abs, currentImageTag, deployKraneNamespace, emptyDir, expandSkipperAlias, isCI, renderKraneNamespace } from "./_utils.ts";

$.cwd = abs();
$.env["SKIPPER_KUBECTL_CONTEXT"] ??= "orbstack";

const flags = parseArgs({
  args: process.argv.slice(2),
  allowNegative: true,
  options: {
    build: { type: "boolean", default: true },
    "generate-paseto-keypair": {
      type: "boolean",
      default: !existsSync(abs("tmp/paseto/private.pem")) || !existsSync(abs("tmp/paseto/public.pem")),
    },
    development: { type: "boolean", default: !isCI },
    help: { type: "boolean", default: false, short: "h" },
    only: { type: "string", default: "controller,router,fixtures,metrics-server,otel-lgtm" },
    otel: { type: "boolean", default: !isCI },
    test: { type: "boolean", default: true },
  },
});

// Expand "skipper" to "controller,router" for convenience
flags.values.only = expandSkipperAlias(flags.values.only);

if (flags.values.help) {
  console.log(dedent`
    Usage:
      deploy [flags]

    Flags:
          --build                    Build images before deploying (${flags.values.build})
          --development              Deploy development namespaces (${flags.values.development})
          --generate-paseto-keypair  Generate a paseto keypair (${flags.values["generate-paseto-keypair"]})
          --only <string>            Only deploy specific components (${flags.values.only})
          --otel                     Enable OpenTelemetry (${flags.values.otel})
          --test                     Deploy test namespaces (${flags.values.test})
      -h, --help                     Show this help message
  `);
  process.exit(0);
}

if (flags.values.build) {
  await import("./build.ts");
}

// Resolve local image IDs so krane detects changes even when the tag is reused.
// Docker doesn't assign digests to local images, so we use the image ID instead.
const imageTag = await currentImageTag();
const controllerImageId = await $stdout`docker images -q skipper-controller:${imageTag}`.catch(() => "");
const routerImageId = await $stdout`docker images -q skipper-router:${imageTag}`.catch(() => "");

if (flags.values["generate-paseto-keypair"]) {
  await emptyDir(abs("tmp/paseto"));
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  await writeFile(abs("tmp/paseto/private.pem"), privateKey.export({ format: "pem", type: "pkcs8" }));
  await writeFile(abs("tmp/paseto/public.pem"), publicKey.export({ format: "pem", type: "spki" }));
  await $`ls -la ${abs("tmp/paseto")}`;
}

const components = new Set(flags.values.only.split(","));

if (components.has("metrics-server")) {
  const clusterRenderDir = await renderKraneNamespace("cluster");
  await $`krane global-deploy "$SKIPPER_KUBECTL_CONTEXT" -f ${clusterRenderDir}/* --selector=app.kubernetes.io/managed-by=krane `;

  const kubeSystemRenderDir = await renderKraneNamespace("kube-system");
  await $`krane deploy kube-system "$SKIPPER_KUBECTL_CONTEXT" -f ${kubeSystemRenderDir}/* --selector=app.kubernetes.io/managed-by=krane --protected-namespaces=default kube-public`;
}

if (components.has("otel-lgtm") && flags.values.otel) {
  await deployKraneNamespace("otel-lgtm");
}

if (components.has("fixtures")) {
  if (flags.values.development) {
    await deployKraneNamespace("skipper-development-fixtures", {
      fixture_image_tag: await currentImageTag(),
      env: {
        OTEL_DENO: flags.values.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }

  if (flags.values.test) {
    await deployKraneNamespace("skipper-test-fixtures", {
      fixture_image_tag: await currentImageTag(),
      env: {
        OTEL_DENO: flags.values.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }
}

if (components.has("controller") || components.has("router")) {
  if (flags.values.development) {
    await deployKraneNamespace("skipper-development", {
      controller_image_name: "skipper-controller",
      router_image_name: "skipper-router",
      image_tag: imageTag,
      controller_annotations: { "skipper/image-id": controllerImageId },
      router_annotations: { "skipper/image-id": routerImageId },
      namespace: "skipper-development",
      function_namespaces: ["skipper-development-fixtures"],
      unsafe_controller_paseto_private_key: await readFile(abs("tmp/paseto/private.pem"), "utf8"),
      router_node_port: 31020,
      controller_node_port: 31021,
      controller_web_node_port: 31022,
      controller_web_template_host_dir: abs("internal/web"),
      env: {
        SKIPPER_TELEMETRY: flags.values.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }

  if (flags.values.test) {
    await deployKraneNamespace("skipper-test", {
      controller_image_name: "skipper-controller",
      router_image_name: "skipper-router",
      image_tag: imageTag,
      controller_annotations: { "skipper/image-id": controllerImageId },
      router_annotations: { "skipper/image-id": routerImageId },
      namespace: "skipper-test",
      function_namespaces: ["skipper-test-fixtures"],
      unsafe_controller_paseto_private_key: await readFile(abs("tmp/paseto/private.pem"), "utf8"),
      router_node_port: 31030,
      controller_node_port: 31031,
      controller_web_node_port: 31032,
      env: {
        SKIPPER_TELEMETRY: flags.values.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }
}
