#!/usr/bin/env -S deno run -A
import { parseArgs } from "jsr:@std/cli/parse-args";
import { abs, currentImageDigest, currentImageTag, deployKraneNamespace, isCI, renderKraneNamespace } from "./_utils.ts";
import { $ } from "npm:zx";
import { emptyDir, existsSync } from "jsr:@std/fs";
import { dedent } from "npm:ts-dedent";
import crypto from "node:crypto";

$.cwd = abs();
$.env.SKIPPER_KUBECTL_CONTEXT ??= "orbstack";

const flags = parseArgs(Deno.args, {
  string: ["only"],
  boolean: ["help", "otel", "development", "test", "generate-paseto-keypair", "build"],
  negatable: ["otel", "development", "test", "generate-paseto-keypair", "build"],
  default: {
    "generate-paseto-keypair": !existsSync(abs("tmp/paseto/private.pem")) || !existsSync(abs("tmp/paseto/public.pem")),
    build: true,
    development: !isCI,
    help: false,
    only: "skipper,fixtures,metrics-server,otel-lgtm",
    otel: !isCI,
    test: true,
  },
  alias: {
    h: "help",
  },
});

if (flags.help) {
  console.log(dedent`
    Usage:
      deploy [flags]

    Flags:
          --build                    Build images before deploying (${flags.build})
          --development              Deploy development namespaces (${flags.development})
          --generate-paseto-keypair  Generate a paseto keypair (${flags["generate-paseto-keypair"]})
          --only <string>            Only deploy specific components (${flags.only})
          --otel                     Enable OpenTelemetry (${flags.otel})
          --test                     Deploy test namespaces (${flags.test})
      -h, --help                     Show this help message
  `);
  Deno.exit(0);
}

if (flags.build) {
  await import("./build.ts");
}

if (flags["generate-paseto-keypair"]) {
  await emptyDir(abs("tmp/paseto"));
  const { publicKey, privateKey } = crypto.generateKeyPairSync("ed25519");
  await Deno.writeTextFile(abs("tmp/paseto/private.pem"), privateKey.export({ format: "pem", type: "pkcs8" }).toString());
  await Deno.writeTextFile(abs("tmp/paseto/public.pem"), publicKey.export({ format: "pem", type: "spki" }).toString());
  await $`ls -la ${abs("tmp/paseto")}`;
}

if (flags.only.includes("metrics-server")) {
  const clusterRenderDir = await renderKraneNamespace("cluster");
  await $`krane global-deploy "$SKIPPER_KUBECTL_CONTEXT" -f ${clusterRenderDir}/* --selector=app.kubernetes.io/managed-by=krane `;

  const kubeSystemRenderDir = await renderKraneNamespace("kube-system");
  await $`krane deploy kube-system "$SKIPPER_KUBECTL_CONTEXT" -f ${kubeSystemRenderDir}/* --selector=app.kubernetes.io/managed-by=krane --protected-namespaces=default kube-public`;
}

if (flags.only.includes("otel-lgtm")) {
  await deployKraneNamespace("otel-lgtm");
}

if (flags.only.includes("fixtures")) {
  if (flags.development) {
    await deployKraneNamespace("skipper-development-fixtures", {
      echo_image_tag: await currentImageTag(),
      echo_image_digest: await currentImageDigest("skipper-fixtures-echo"),
      env: {
        OTEL_DENO: flags.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }

  if (flags.test) {
    await deployKraneNamespace("skipper-test-fixtures", {
      echo_image_tag: await currentImageTag(),
      echo_image_digest: !isCI ? await currentImageDigest("skipper-fixtures-echo") : undefined,
      env: {
        OTEL_DENO: flags.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }
}

if (flags.only.includes("skipper")) {
  if (flags.development) {
    await deployKraneNamespace("skipper-development", {
      image_tag: await currentImageTag(),
      image_digest: await currentImageDigest("skipper"),
      namespace: "skipper-development",
      function_namespaces: ["skipper-development-fixtures"],
      unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
      router_node_port: 31020,
      controller_node_port: 31021,
      env: {
        SKIPPER_TELEMETRY: flags.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }

  if (flags.test) {
    await deployKraneNamespace("skipper-test", {
      image_tag: await currentImageTag(),
      image_digest: !isCI ? await currentImageDigest("skipper") : undefined,
      namespace: "skipper-test",
      function_namespaces: ["skipper-test-fixtures"],
      unsafe_controller_paseto_private_key: await Deno.readTextFile(abs("tmp/paseto/private.pem")),
      router_node_port: 31030,
      controller_node_port: 31031,
      env: {
        SKIPPER_TELEMETRY: flags.otel,
        OTEL_EXPORTER_OTLP_PROTOCOL: "http/protobuf",
        OTEL_EXPORTER_OTLP_ENDPOINT: "http://otel-lgtm.otel-lgtm.svc.cluster.local:4318",
      },
    });
  }
}
