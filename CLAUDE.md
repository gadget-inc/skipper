# CLAUDE.md

## Commands

All commands must be run with `direnv exec .`:

```bash
direnv exec . dev up                              # Run controller, router, and docs locally
direnv exec . dev up --only=controller            # Controller only
direnv exec . dev up --only=controller,router     # Controller + router (no docs)
direnv exec . dev up --only=docs                  # Docs dev server only
direnv exec . dev deploy                          # Build and deploy to Kubernetes (Orbstack)
direnv exec . dev deploy --only=skipper           # Deploy only Skipper
direnv exec . dev fixture request                 # Send an HTTP request to the fixture
direnv exec . dev fixture websocket               # Open a WebSocket to the fixture
direnv exec . dev fixture load -c 10 -n 1000      # Load test the fixture
direnv exec . dev build                           # Build controller, router, and fixture images
direnv exec . dev build --only=fixtures           # Build only the fixture image
direnv exec . dev kube-lint                       # Render template.yaml.erb and lint each binding configuration
direnv exec . dev tests go ./...                      # Run all Go tests
direnv exec . dev tests go -short ./...               # Run Go tests without Kubernetes (skips integration tests)
direnv exec . dev tests go -v ./internal/controller/... # Run specific package tests
direnv exec . dev tests docs                          # Run docs tests
direnv exec . dev tests scripts                       # Run script tests (vitest)
direnv exec . dev tests e2e                           # Run e2e tests
direnv exec . dev tests all                           # Run go + docs + scripts (not e2e)
direnv exec . dev lint                            # Run golangci-lint, oxfmt, oxlint
direnv exec . dev fmt                             # Auto-fix formatting
direnv exec . dev generate                        # Regenerate protobuf Go code from .proto files
direnv exec . dev logs                            # Show recent logs and exit
direnv exec . dev logs -f                         # Stream logs continuously (follow)
direnv exec . dev logs -c controller              # Show controller logs
direnv exec . dev logs --errors --since=5m        # Recent errors only
direnv exec . dev docs                            # Start docs dev server
direnv exec . dev docs build                      # Build docs site
direnv exec . dev docs preview                    # Preview built docs
direnv exec . dev clean                           # Delete all Kubernetes resources
direnv exec . profile fetch                       # Fetch heap profile from local controller
direnv exec . profile fetch -t cpu -p             # Fetch CPU profile from production
direnv exec . profile fetch -t cpu -p <pod>       # Fetch from a specific production pod
direnv exec . profile fetch -t cpu -p --spread    # CPU profiles from all production pods
direnv exec . profile open <file>                 # Open a saved profile in browser
direnv exec . profile merge                       # Merge CPU profiles into default.pgo
direnv exec . profile merge --clean               # Merge and remove source profiles
direnv exec . profile merge --dry-run             # Preview merge without writing
direnv exec . profile analyze --pgo               # Top hotspots in committed PGO profile
direnv exec . profile analyze --pgo --cum         # Sort by cumulative time
direnv exec . profile analyze --pgo -c router     # Analyze router PGO profile
direnv exec . profile analyze --pgo --mode=peek -f Hash  # Callers/callees of Hash
direnv exec . profile analyze --pgo --mode=source -f Hash # Source-annotated view
direnv exec . profile analyze <file>              # Analyze any .pb.gz profile
direnv exec . profile analyze --mode=diff --diff-base=before.pb.gz after.pb.gz  # Compare profiles
```

## Local Development

First-time setup (fixtures and metrics-server still run in K8s):

```bash
direnv exec . dev deploy --only=fixtures,metrics-server
```

`dev up` runs controller, router, Tailwind, and the docs site as local processes. Local endpoints:

- Controller gRPC: `127.0.0.1:50051`
- Web UI: `http://127.0.0.1:8080`
- Router: `http://127.0.0.1:8081`
- Docs: `http://localhost:4321`

### Debugging

`dev up` writes structured JSON logs to `tmp/logs/controller.jsonl` and `tmp/logs/router.jsonl`. Any command also accepts:

```bash
direnv exec . controller --log-file=tmp/logs/controller.jsonl                    # Write logs to file and stderr
direnv exec . controller --log-file=tmp/logs/controller.jsonl --log-file-level=trace  # File at trace, stderr at default
direnv exec . controller --log-file=tmp/logs/controller.jsonl --log-file-format=text  # File as text, stderr as default
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

- `internal/hashring/`: Consistent hashing for deterministic routing (1024 virtual nodes per IP)
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
