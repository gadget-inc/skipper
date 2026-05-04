# CLAUDE.md

## Commands

**Commands.** `direnv allow` (run once after cloning) auto-loads the dev shell when you `cd` into the repo, so every command in this file Just Works as written. From contexts that don't inherit a shell (some CI runners, agent harnesses), prefix any command with `direnv exec .` to load the same environment. This paragraph is the single source of truth for that qualification -- the rest of this file shows the bare command form.

```bash
dev up                              # Run controller, router, and docs locally
dev up --only=controller            # Controller only
dev up --only=controller,router     # Controller + router (no docs)
dev up --only=docs                  # Docs dev server only
dev deploy                          # Build and deploy to Kubernetes (Orbstack)
dev deploy --only=skipper           # Deploy only Skipper
dev fixture request                 # Send an HTTP request to the fixture
dev fixture websocket               # Open a WebSocket to the fixture
dev fixture load -c 10 -n 1000      # Load test the fixture
dev build                           # Build controller, router, and fixture images
dev build --only=fixtures           # Build only the fixture image
dev kube-lint                       # Render template.yaml.erb and lint each binding configuration
dev test                                # Run all Go tests
dev test -short ./...                   # Run Go tests without Kubernetes (skips integration tests)
dev test -v ./internal/controller/...   # Run specific package tests
dev test e2e                            # Run e2e tests
dev lint                            # Run kube-lint, lint-docs, and golangci-lint
dev lint-docs                       # Lint repository documentation
dev fmt                             # Auto-fix formatting (golangci-lint fmt)
dev generate                        # Regenerate protobuf Go code from .proto files
dev logs                            # Show recent logs and exit
dev logs -f                         # Stream logs continuously (follow)
dev logs -c controller              # Show controller logs
dev logs --errors --since=5m        # Recent errors only
dev docs                            # Start docs dev server
dev docs build                      # Build docs site
dev clean                           # Delete all Kubernetes resources
dev profile fetch                       # Fetch heap profile from local controller
dev profile fetch -t cpu -p             # Fetch CPU profile from production
dev profile fetch -t cpu -p <pod>       # Fetch from a specific production pod
dev profile fetch -t cpu -p --spread    # CPU profiles from all production pods
dev profile open <file>                 # Open a saved profile in browser
dev profile merge                       # Merge CPU profiles into default.pgo
dev profile merge --clean               # Merge and remove source profiles
dev profile merge --dry-run             # Preview merge without writing
dev profile analyze --pgo               # Top hotspots in committed PGO profile
dev profile analyze --pgo --cum         # Sort by cumulative time
dev profile analyze --pgo -c router     # Analyze router PGO profile
dev profile analyze --pgo --mode=peek -f Hash  # Callers/callees of Hash
dev profile analyze --pgo --mode=source -f Hash # Source-annotated view
dev profile analyze <file>              # Analyze any .pb.gz profile
dev profile analyze --mode=diff --diff-base=before.pb.gz after.pb.gz  # Compare profiles
```

## Local Development

First-time setup (fixtures and metrics-server still run in K8s):

```bash
dev deploy --only=fixtures,metrics-server
```

`dev up` runs controller, router, and the docs site as local processes. Local endpoints:

- Controller gRPC: `127.0.0.1:50051`
- Web UI: `http://127.0.0.1:8080`
- Router: `http://127.0.0.1:8081`
- Docs: `http://localhost:4321`

### Debugging

`dev up` writes structured JSON logs to `tmp/logs/controller.jsonl` and `tmp/logs/router.jsonl`. Any command also accepts:

```bash
controller --log-file=tmp/logs/controller.jsonl                    # Write logs to file and stderr
controller --log-file=tmp/logs/controller.jsonl --log-file-level=trace  # File at trace, stderr at default
controller --log-file=tmp/logs/controller.jsonl --log-file-format=text  # File as text, stderr as default
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
