#!/usr/bin/env -S deno run -A
import { deployKraneNamespace, isCI } from "./_utils.ts";

// no need to deploy if running in CI
if (!isCI) {
  await deployKraneNamespace("otel-lgtm");
}
