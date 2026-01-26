#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types

import { $ } from "zx";
import { abs } from "./_utils.ts";

$.cwd = abs();

await $`buf generate`;
