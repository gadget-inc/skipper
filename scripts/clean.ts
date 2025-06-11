#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import { rm } from "node:fs/promises";
import { $ } from "zx";
import { abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.env["SKIPPER_KUBE_CONTEXT"] ??= "orbstack";

await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete namespace skipper-development --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete namespace skipper-development-fixtures --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete namespace skipper-test --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete namespace skipper-test-fixtures --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete namespace otel-lgtm --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete -f deploy/cluster/metrics-server.yaml --ignore-not-found`;
await $`kubectl --context="$SKIPPER_KUBE_CONTEXT" delete -f deploy/kube-system/metrics-server.yaml --ignore-not-found`;

await rm(abs("tmp/krane"), { recursive: true, force: true });
await rm(abs("tmp/logs"), { recursive: true, force: true });
await rm(abs("tmp/paseto"), { recursive: true, force: true });
await rm(abs("tmp/test"), { recursive: true, force: true });
