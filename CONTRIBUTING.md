# Contributing

## Quick Start

1. Install [Nix](https://nixos.org/download), [direnv](https://direnv.net/), and [Orbstack](https://orbstack.dev/).
2. `cd` into the repository and run `direnv allow`.
3. Run `pnpm install` to install Node.js dependencies.
4. Run `deploy` to build and deploy to your local Kubernetes cluster.
5. Run `tests -short ./...` to verify everything works.

## Full Guide

See the [Contributing section](docs/src/content/docs/contributing/) in the docs for the complete guide, including:

- [Building and Deploying](docs/src/content/docs/contributing/building-and-deploying.mdx) — deploy scripts, echo smoke tests, cleanup
- [Testing, Linting, and Code Generation](docs/src/content/docs/contributing/testing.mdx) — test commands, linting, code generation
- [Watching Logs](docs/src/content/docs/contributing/watching-logs.mdx) — stream and filter pod logs
- [Profiling and PGO](docs/src/content/docs/contributing/profiling.mdx) — collect profiles, generate PGO data
- [Contribution Workflow](docs/src/content/docs/contributing/workflow.mdx) — PRs, CI, Docker image builds
