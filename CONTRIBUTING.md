# Contributing

## Quick Start

1. Install [Nix](https://nixos.org/download), [direnv](https://direnv.net/), and [Orbstack](https://orbstack.dev/).
2. `cd` into the repository and run `direnv allow`.
3. Run `git config core.hooksPath .githooks` to enable the repo's git hooks (`dev lint` on commit, `dev test -short ./...` on push). Bypass with `--no-verify` when needed.
4. Run `dev deploy` to build and deploy to your local Kubernetes cluster.
5. Run `dev test -short ./...` to verify everything works.

## Debugging

When running `dev`, controller and router logs are written to `tmp/logs/controller.jsonl` and `tmp/logs/router.jsonl` as structured JSON. These files are truncated on each restart.

You can also pass `--log-file`, `--log-file-level`, and `--log-file-format` to any command to write logs to a file. When `--log-file` is set, logs go to both stderr and the file. Level and format default to the main `--log-level` and `--log-format`.

## Full Guide

See the [Contributing section](docs/content/contributing/) in the docs for the complete guide, including:

- [Getting Started](docs/content/contributing/getting-started.md) — prerequisites and setup
- [Building and Deploying](docs/content/contributing/building-and-deploying.md) — deploy commands, fixture smoke tests, cleanup
- [Testing, Linting, and Code Generation](docs/content/contributing/testing.md) — test commands, linting, code generation
- [Watching Logs](docs/content/contributing/watching-logs.md) — stream and filter pod logs
- [Web UI](docs/content/contributing/web-ui.md) — dev mode, available routes, live-reload
- [Profiling and PGO](docs/content/contributing/profiling.md) — collect profiles, generate PGO data
- [Contribution Workflow](docs/content/contributing/workflow.md) — PRs, CI, Docker image builds
