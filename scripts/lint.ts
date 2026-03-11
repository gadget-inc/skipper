#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { $ } from "zx";
import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await $nothrow`scripts/kube-lint.ts`;
await $nothrow`golangci-lint run`;
await $nothrow`pnpm --dir docs exec astro sync`;
await $nothrow`oxfmt --check .`;
await $nothrow`oxlint --type-aware --type-check --max-warnings=0 .`;
