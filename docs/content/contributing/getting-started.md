---
title: Getting Started
description: Set up your development environment for Skipper.
---

## Prerequisites

- [Nix](https://nixos.org/download)
- [direnv](https://direnv.net/)
- [Orbstack](https://orbstack.dev/) (for running Kubernetes locally)

## Setup

1. Install the prerequisites above.
2. `cd` into the repository.
3. Run `direnv allow` — direnv will automatically enter the `nix develop` shell that contains all the tools you need to build and run the project.

Every new terminal opened in the repository will automatically provide the correct environment.

## Project layout

<div class="table-scroll">

| Path                | Description                                                    |
| ------------------- | -------------------------------------------------------------- |
| `cmd/`              | Binary entry points (`controller`, `router`, `fixture`, `dev`) |
| `docs/content/`     | Documentation site source (Markdown)                           |
| `internal/`         | Skipper source code (Go)                                       |
| `internal/web/`     | Web UI server, handlers, and HTML templates                    |
| `nix/`              | Nix development environment                                    |
| `tmp/`              | Temporary build artifacts (safe to delete)                     |
| `template.yaml.erb` | Krane deployment template                                      |

</div>
