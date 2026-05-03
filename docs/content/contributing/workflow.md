---
title: Contribution Workflow
description: How to submit changes and build Docker images.
---

## Submitting changes

1. Create a new branch and make your changes.
2. Confirm `deploy`, `tests`, `lint`, and `fmt` all succeed locally.
3. Open a Pull Request — GitHub Actions will run the same scripts in CI.
4. One approval from a maintainer and passing CI are required to merge.

If you add a new dependency or a new development tool, please also update `nix/flake.nix`.

## Building and pushing Docker images

Docker images are built and pushed manually via GitHub Actions. To trigger a build:

1. Go to the [Actions](https://github.com/gadget-inc/skipper/actions) tab in GitHub.
2. Select the **release** workflow from the sidebar.
3. Click **Run workflow**, optionally specify a git ref (commit SHA, branch, or tag), and confirm.

This will build multi-architecture images (amd64 and arm64) and push them to Google Artifact Registry. Images are tagged with the short commit SHA (e.g., `sha-abc1234`).

CI runs automatically on every pull request and push to `main`. Docker builds do not run automatically — they must be triggered manually as described above.

## Documentation

Documentation is built by an in-tree static-site generator (`internal/dev/docssite`, Goldmark-based). Source files live under `docs/content/` as plain Markdown files.

```bash
dev docs        # Start the dev server with live reload
dev docs build  # Build the static site to docs/dist/
```
