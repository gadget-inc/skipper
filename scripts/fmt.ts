#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { $ } from "zx";

import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await $nothrow`golangci-lint fmt`;
await $nothrow`pnpm exec oxfmt --write .`;
await $nothrow`pnpm exec oxlint --type-aware --type-check --fix .`;
