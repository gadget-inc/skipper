# CLAUDE.md

## Commands

All commands must be run with `direnv exec .`:

```bash
direnv exec . deploy                              # Build and deploy to Kubernetes (Orbstack)
direnv exec . deploy --only=skipper               # Deploy only Skipper
direnv exec . tests ./...                         # Run all tests
direnv exec . tests -short ./...                  # Run tests without Kubernetes (skips integration tests)
direnv exec . tests -v ./internal/controller/...  # Run specific package tests
direnv exec . lint                                # Run golangci-lint, oxfmt, oxlint
direnv exec . fmt                                 # Auto-fix formatting
direnv exec . buf generate                        # Regenerate protobuf Go code from .proto files
direnv exec . logs                                # Show recent logs and exit
direnv exec . logs -f                             # Stream logs continuously (follow)
direnv exec . logs -c controller                  # Show controller logs
direnv exec . logs --errors --since=5m            # Recent errors only
direnv exec . clean                               # Delete all Kubernetes resources
```

## Architecture

Skipper is a Kubernetes controller that turns deployments into a pool of functions assignable to tenants.

### Two-Component Design

**Controller** (`internal/controller/`): Runs in Kubernetes, discovers deployments with `skipper/deployment` label, manages pod lifecycle and scaling. Uses informers for Kubernetes events, assigns pods to functions via PASETO-signed tokens, and implements HPA-inspired scaling based on CPU, memory, and request metrics.

**Router** (`internal/router/`): Receives HTTP requests with `x-skipper-function` header, routes to assigned instances via consistent hashing, proxies all traffic (HTTP/WebSocket), sends heartbeats to prevent function timeout.

### Core Domain Models (`internal/skipper/`)

- **Function**: Identified by namespace + deployment + tenant + metadata, produces a unique `FunctionHash` (uint64)
- **Instance**: An assigned pod with IP, port, timestamps, and resource metrics
- **Heartbeat**: Router→Controller signal indicating active requests

### Key Internal Packages

- `internal/hashring/`: Consistent hashing for deterministic routing (4096 virtual nodes per IP)
- `internal/key/`: Type-safe structured logging keys with OpenTelemetry conversion
- `internal/telemetry/`: OTLP tracing and Prometheus metrics
- `internal/config/`: Declarative flag binding with struct tags

### Request Flow

1. Client → Router with `x-skipper-function` header
2. Router queries Controller for instance assignment
3. Controller finds/assigns pod, returns instance address
4. Router proxies request to pod, sends heartbeats during activity
5. Controller terminates pods after heartbeat timeout (default 90s)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines and contribution instructions.
