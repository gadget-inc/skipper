---
title: Testing, Linting, and Code Generation
description: Run tests, linters, and regenerate protobuf code.
---

## Testing

```bash
dev tests go                                  # Runs go test ./...
dev tests go -v ./internal/controller/...     # Verbose tests for the controller package
dev tests go -short ./...                     # Skips integration tests (no Orbstack required)
```

`dev tests go` forwards all arguments to `go test`.

:::tip
Some tests are integration tests that require Skipper to be deployed to Orbstack. Pass `-short` to skip these tests if you haven't deployed yet.
:::

## Linting and formatting

```bash
dev lint    # Runs kube-lint, lint-docs, and golangci-lint
dev fmt     # Attempts to fix linting errors
```

All Go files must be formatted with **gofumpt** (enforced by `golangci-lint`).

## Code generation

### Protocol Buffers

Core domain types are defined in `internal/skipper/types.proto` and the gRPC service is defined in `internal/skipper/controller.proto`. After modifying `.proto` files, regenerate the Go code:

```bash
buf generate
```

This uses [Buf](https://buf.build/) with the configuration in `buf.gen.yaml` to generate:

- `types.pb.go` — Go structs and enums (from types.proto)
- `types.pb.json.go` — JSON marshaling helpers (from types.proto)
- `controller.pb.go` — Request/response types (from controller.proto)
- `controller.pb.json.go` — JSON marshaling helpers (from controller.proto)
- `controller_grpc.pb.go` — gRPC service stubs (from controller.proto)
