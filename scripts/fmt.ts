#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { $ } from "zx";
import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await $nothrow`golangci-lint fmt`;
await $nothrow`prettier --write .`;
await $nothrow`eslint --fix .`;
