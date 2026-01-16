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

To watch the logs of Skipper pods in a given namespace, run:

```bash
$ logs                                 # Streams logs from all Skipper pods.
$ logs --namespace=skipper-development # Streams logs from the development namespace.
$ logs | grep <trace-id>               # Streams logs by trace ID. (e.g., `logs | grep 64627b12057c02db7a60d82a9dbf74cb`)
```

> [!NOTE]
> The `logs` command takes over your terminal with a continuous stream of logs. It's recommended to run this in a separate terminal tab or window while you run other commands in your main terminal.

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
1. Select the **cd** workflow from the sidebar.
1. Click **Run workflow** and confirm.

This will run CI first, then build multi-architecture images (amd64 and arm64) and push them to Google Artifact Registry. Images are tagged with the short commit SHA (e.g., `sha-abc1234`).

> [!NOTE]
> CI runs automatically on every pull request and push to `main`. Docker builds do not run automatically — they must be triggered manually as described above.

## Contribution Workflow

1. Create a new branch and make your changes.
1. Confirm `deploy`, `tests`, `lint`, and `fmt` all succeed locally.
1. Open a Pull Request — GitHub Actions will run the same scripts in CI.
1. One approval from a maintainer and passing CI are required to merge.

If you add a new dependency or a new development tool, please also update `nix/flake.nix`.

Thanks for contributing!
