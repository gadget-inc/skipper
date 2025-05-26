#!/usr/bin/env -S node --no-warnings --experimental-strip-types
import { $ } from "zx";
import { $nothrow, abs } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";

await $nothrow`
cd internal/function
protoc scale.proto function.proto instance.proto heartbeat.proto \\
    --proto_path=./ \\
    --go_out=. \\
    --go_opt=paths=source_relative \\
    --go_opt=default_api_level=API_OPAQUE
`;

await $nothrow`
cd internal/controller
protoc controller.proto \\
    --proto_path=./ \\
    --proto_path=../function \\
    --go_out=. \\
    --go_opt=paths=source_relative \\
    --go_opt=default_api_level=API_OPAQUE \\
    --go-grpc_out=. \\
    --go-grpc_opt=paths=source_relative
`;
