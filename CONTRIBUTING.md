# Contributing

This project is developed primarily in [Go](https://go.dev/) with helper scripts written in [Node.js](https://nodejs.org/). The project provides a declarative development environment via [direnv](https://direnv.net/) and [Nix flakes](https://nix.dev/concepts/flakes), so anyone can get an identical toolchain with a single command.

## Getting Started

### Prerequisites

- [Nix](https://nixos.org/download)
- [direnv](https://direnv.net/)
- [Orbstack](https://orbstack.dev/) (for running Kubernetes locally)

### Setup

1. Install the prerequisites above.
1. `cd` into the repository.
1. Run `direnv allow` – direnv will automatically enter the `nix develop` shell that contains all the tools you need to build and run the project.
1. Run `pnpm install` to install Node.js dependencies.

Every new terminal opened in the repository will automatically provide the correct environment.

## Project Layout Cheat Sheet

```
/deploy              → Krane-rendered Kubernetes manifests (YAML)
/fixtures            → Example echo servers used in integration tests (Node.js)
/internal            → Skipper implementation (Go)
/nix                 → Nix development environment (Nix)
/scripts             → Developer tooling (Node.js)
/tmp                 → Temporary artifacts (safe to delete)
template.yaml.erb    → Krane template to deploy Skipper (YAML)
```

## Building & Running

> [!NOTE]
> Skipper is designed to run inside Kubernetes. You should not run the code natively (e.g., with `go run .`). All changes should be built and deployed using the provided scripts.

### Deploying to Kubernetes (Orbstack)

To deploy all the necessary resources to your local Kubernetes cluster, run:

```bash
$ deploy
```

This will build and deploy everything in the [`deploy/`](deploy/) directory. The script lives at `scripts/deploy.ts` and has many options. For example, to deploy only Skipper, run:

```bash
$ deploy --only=skipper
```

Any code changes you make will be picked up when you run `deploy` again. The `deploy` script will automatically build the latest code and update your Kubernetes environment.

See `deploy --help` for available flags.

## Smoke Testing with Echo Scripts

A set of `echo-*` scripts are provided in the [`scripts/`](scripts/) directory to help you quickly smoke test your Skipper deployment. These scripts interact with the deployed echo fixture and can be used to verify basic request routing, WebSocket handling, and load balancing.

### Available Echo Scripts

#### 1. `echo-request`

Sends a simple HTTP request to the echo function via Skipper's router and prints the response.

**Example:**

```bash
$ echo-request
```

#### 2. `echo-websocket`

Opens a WebSocket connection to the echo function via Skipper's router, sends a test message, and prints the echoed response.

**Example:**

```bash
$ echo-websocket
```

#### 3. `echo-load-test`

Performs a basic load test by sending multiple concurrent requests to the echo function via Skipper's router. Useful for verifying load balancing and basic performance.

**Example:**

```bash
$ echo-load-test
```

These scripts are implemented in Node.js and are available as commands in your development shell. They are a quick way to verify that Skipper is routing requests correctly and that the echo fixture is functioning as expected.

All `*.local.*` files are ignored by `.gitignore` so you can duplicate and create your own scripts without committing them. (e.g., `echo-request.local.ts`)

### Watching Logs

To watch the logs of Skipper pods, run:

```bash
$ logs                                        # Streams logs from all Skipper pods.
$ logs -c controller                          # Streams logs from controller pods only.
$ logs -c router --level=warn                 # Streams router warnings and errors.
$ logs --grep="trace_id=<id>"                 # Filter logs by trace ID (native, no pipe needed).
$ logs --errors --since=5m                    # Show errors from last 5 minutes and exit.
```

See `logs --help` for all available flags including output formats and filtering options.

> [!NOTE]
> By default, `logs` shows current logs and exits. Use `-f` or `--follow` to stream continuously.

## Testing

```bash
$ tests # Runs `go test ./...`.
```

`tests` forwards all arguments to `go test`:

```bash
$ tests -v ./internal/controller/...  # Runs verbose tests only for the controller package.
$ tests -short ./...                  # Skips integration tests (no Orbstack required).
```

> [!TIP]
> Some tests are integration tests that require Skipper to be deployed to Orbstack. Pass `-short` to skip these tests if you haven't deployed yet.

## Linting & Formatting

```bash
$ lint # Runs golangci-lint, oxfmt, and oxlint (with type checking).
$ fmt  # Attempts to fix linting errors.
```

All Go files must be formatted with **gofumpt** (enforced by `golangci-lint`).

## Code Generation

### Protocol Buffers

Core domain types are defined in [`internal/skipper/types.proto`](internal/skipper/types.proto). After modifying the `.proto` file, regenerate the Go code:

```bash
$ buf generate
```

This uses [Buf](https://buf.build/) with the configuration in [`buf.gen.yaml`](buf.gen.yaml) to generate:

- `types.pb.go` – Go structs and enums
- `types_grpc.pb.go` – gRPC service stubs (if services are defined)
- `types.pb.json.go` – JSON marshaling helpers

## Profiling & PGO

Skipper supports [profile-guided optimization](https://go.dev/doc/pgo) (PGO), typically yielding 2–14% CPU improvement. The `profile` command collects pprof profiles from running pods and merges them into `default.pgo` files that the Go compiler uses to optimize builds.

There are two profile types:

| Type     | Flag                    | Purpose                                         |
| -------- | ----------------------- | ----------------------------------------------- |
| **Heap** | `--type=heap` (default) | Debug memory usage during development           |
| **CPU**  | `--type=cpu`            | Generate PGO data — collect from **production** |

> [!NOTE]
> **Why production profiles?** Development profiles reflect test fixtures and artificial load, not real user traffic. PGO is most effective when the compiler sees the hot paths that matter in production.

### Collecting Profiles

```bash
$ profile fetch                              # Heap profile from local controller (for debugging)
$ profile fetch --type=cpu                   # CPU profile from local controller (30s)
$ profile fetch --type=cpu --production      # CPU profile from production (for PGO)
$ profile fetch --type=cpu -p --seconds=60   # Custom duration
$ profile fetch --component=router           # Profile the router instead
$ profile fetch --web                        # Fetch and immediately open in browser
$ profile fetch --type=heap --diff           # Fetch and compare against previous profiles
```

#### Targeting Specific Pods

Use `--spread` to fetch one profile from every pod — this is the recommended approach for PGO:

```bash
$ profile fetch --type=cpu --production --spread
```

> [!NOTE]
> Transient connection resets are common with `--spread` (e.g., 3 of 6 pods may fail). The script prints a short error per pod and copy-pasteable retry commands.

To target a single pod instead, pass its name as a positional argument:

```bash
$ kubectl --context=gke_gadget-core-production_us-central1_main -n skipper-production get pods
$ profile fetch --type=cpu --production skipper-production-controller-7f9b8c6d5-abc12
```

#### Where Profiles Are Saved

Profiles are saved to `tmp/pprof/<environment>/<component>/` with auto-incrementing filenames. Production and development profiles are kept in separate directories so that `merge` only sees production data:

```
tmp/pprof/production/controller/pod-name-cpu-001.pb.gz
tmp/pprof/development/controller/pod-name-heap-001.pb.gz
```

### Viewing Profiles

```bash
$ profile open tmp/pprof/<environment>/<component>/<file>.pb.gz        # Open in pprof web UI
$ profile open tmp/pprof/<environment>/<component>/<file>.pb.gz --diff # Compare against earlier profiles
```

### Generating PGO Files

After collecting representative CPU profiles from production:

```bash
$ profile merge                          # Merge all component profiles into cmd/*/default.pgo
$ profile merge --component=controller   # Merge only controller
$ profile merge --dry-run                # Preview what would be merged
```

`merge` only reads from `tmp/pprof/production/` — development profiles are never included.

### End-to-End PGO Workflow

> [!IMPORTANT]
> Use the same `--seconds` value for all profiles in a collection round. Different durations skew the merge because longer profiles contribute disproportionately more samples.

1. **Collect controller profiles** from production, spread across all pods:

   ```bash
   $ profile fetch --type=cpu --production --spread
   ```

   If some pods fail, successful profiles are still saved — re-run for specific pods or proceed with what you have.

2. **Collect router profiles** (can run in parallel from a separate terminal):

   ```bash
   $ profile fetch --type=cpu --production --component=router --spread
   ```

3. **Preview and merge:**

   ```bash
   $ profile merge --dry-run
   $ profile merge --clean       # --clean removes source profiles after a successful merge
   ```

   If `merge` warns that profiles span more than 7 days, delete stale files from `tmp/pprof/production/` and collect fresh ones before merging.

4. **Verify** the merged profiles:

   ```bash
   $ direnv exec . go tool pprof -top cmd/controller/default.pgo | head -20
   $ direnv exec . go tool pprof -top cmd/router/default.pgo | head -20
   ```

   Check that `Type: cpu`, Duration is roughly `--seconds × pod count`, and the top functions include application or library code (not only `runtime.*`).

5. **Commit** the updated `cmd/*/default.pgo` files — Go automatically uses `default.pgo` when present.

### Good to Know

- **Stale profiles are safe.** The compiler falls back to default optimization for functions not covered by the profile, so stale profiles won't make things slower — the benefit just decreases over time as hot paths drift. Refresh after major refactors.
- **Iterative stability.** It's safe to collect profiles from PGO-optimized binaries and use them for the next build. Go's PGO converges, so there's no need for a two-stage canary process.
- **Cross-platform.** Profiles collected from Linux production pods work for builds targeting any OS or architecture. The Dockerfile builds multi-arch images (amd64/arm64), and both benefit from the same profiles.

See `profile --help` and `profile <command> --help` for all available flags.

## Cleaning Up Resources

To clean up all deployed resources and temporary files, run:

```bash
$ clean
```

This script will:

- Delete all Skipper-related Kubernetes namespaces (development, test, and fixtures).
- Remove metrics server resources.
- Clear temporary files (logs, test artifacts, etc.).

Use this when you want to start fresh or remove all traces of Skipper from your local Kubernetes cluster.

## Building & Pushing Docker Images

Docker images are built and pushed manually via GitHub Actions. To trigger a build:

1. Go to the [Actions](../../actions) tab in GitHub.
1. Select the **release** workflow from the sidebar.
1. Click **Run workflow**, optionally specify a git ref (commit SHA, branch, or tag), and confirm.

This will build multi-architecture images (amd64 and arm64) and push them to Google Artifact Registry. Images are tagged with the short commit SHA (e.g., `sha-abc1234`).

> [!NOTE]
> CI runs automatically on every pull request and push to `main`. Docker builds do not run automatically — they must be triggered manually as described above.

## Contribution Workflow

1. Create a new branch and make your changes.
1. Confirm `deploy`, `tests`, `lint`, and `fmt` all succeed locally.
1. Open a Pull Request — GitHub Actions will run the same scripts in CI.
1. One approval from a maintainer and passing CI are required to merge.

If you add a new dependency or a new development tool, please also update `nix/flake.nix`.

Thanks for contributing!
