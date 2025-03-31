#!/usr/bin/env -S deno run -A
import { $ } from "npm:zx";

$.verbose = true;

await $`kubectl --context=orbstack delete namespace skipper-development --ignore-not-found`;
await $`kubectl --context=orbstack delete namespace skipper-development-fixtures --ignore-not-found`;
await $`kubectl --context=orbstack delete namespace skipper-test --ignore-not-found`;
await $`kubectl --context=orbstack delete namespace skipper-test-fixtures --ignore-not-found`;
await $`kubectl --context=orbstack delete namespace otel-lgtm --ignore-not-found`;
