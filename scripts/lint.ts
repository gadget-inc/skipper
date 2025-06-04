#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import { $ } from "zx";
import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await $nothrow`golangci-lint run`;
await $nothrow`prettier --check .`;
await $nothrow`eslint --max-warnings=0 .`;
await $nothrow`tsc --project .`;
