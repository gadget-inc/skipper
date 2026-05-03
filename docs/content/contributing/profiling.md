---
title: Profiling and PGO
description: Collect profiles and generate profile-guided optimization data.
---

Skipper supports [profile-guided optimization](https://go.dev/doc/pgo) (PGO), typically yielding 2--14% CPU improvement. The `dev profile` command collects pprof profiles from running pods and merges them into `default.pgo` files that the Go compiler uses to optimize builds.

There are two profile types:

| Type     | Flag                    | Purpose                                         |
| -------- | ----------------------- | ----------------------------------------------- |
| **Heap** | `--type=heap` (default) | Debug memory usage during development           |
| **CPU**  | `--type=cpu`            | Generate PGO data — collect from **production** |

:::note
**Why production profiles?** Development profiles reflect test fixtures and artificial load, not real user traffic. PGO is most effective when the compiler sees the hot paths that matter in production.
:::

## Collecting profiles

```bash
dev profile fetch                              # Heap profile from local controller (for debugging)
dev profile fetch --type=cpu                   # CPU profile from local controller (30s)
dev profile fetch --type=cpu --production      # CPU profile from production (for PGO)
dev profile fetch --type=cpu -p --seconds=60   # Custom duration
dev profile fetch --component=router           # Profile the router instead
dev profile fetch --web                        # Fetch and immediately open in browser
dev profile fetch --type=heap --diff           # Fetch and compare against previous profiles
```

### Targeting specific pods

Use `--spread` to fetch one profile from every pod — this is the recommended approach for PGO:

```bash
dev profile fetch --type=cpu --production --spread
```

:::note
Transient connection resets are common with `--spread` (e.g., 3 of 6 pods may fail). The command prints a short error per pod and copy-pasteable retry commands.
:::

To target a single pod instead, pass its name as a positional argument:

```bash
kubectl --context=gke_gadget-core-production_us-central1_main -n skipper-production get pods
dev profile fetch --type=cpu --production skipper-production-controller-7f9b8c6d5-abc12
```

### Where profiles are saved

Profiles are saved to `tmp/pprof/<environment>/<component>/` with auto-incrementing filenames. Production and development profiles are kept in separate directories so that `merge` only sees production data:

```
tmp/pprof/production/controller/pod-name-cpu-001.pb.gz
tmp/pprof/development/controller/pod-name-heap-001.pb.gz
```

## Viewing profiles

```bash
dev profile open tmp/pprof/<environment>/<component>/<file>.pb.gz        # Open in pprof web UI
dev profile open tmp/pprof/<environment>/<component>/<file>.pb.gz --diff # Compare against earlier profiles
```

## Generating PGO files

After collecting representative CPU profiles from production:

```bash
dev profile merge                          # Merge all component profiles into cmd/*/default.pgo
dev profile merge --component=controller   # Merge only controller
dev profile merge --dry-run                # Preview what would be merged
```

`merge` only reads from `tmp/pprof/production/` — development profiles are never included.

## End-to-end PGO workflow

:::caution
Use the same `--seconds` value for all profiles in a collection round. Different durations skew the merge because longer profiles contribute disproportionately more samples.
:::

1. **Collect controller profiles** from production, spread across all pods:

   ```bash
   dev profile fetch --type=cpu --production --spread
   ```

   If some pods fail, successful profiles are still saved — re-run for specific pods or proceed with what you have.

2. **Collect router profiles** (can run in parallel from a separate terminal):

   ```bash
   dev profile fetch --type=cpu --production --component=router --spread
   ```

3. **Preview and merge:**

   ```bash
   dev profile merge --dry-run
   dev profile merge --clean       # --clean removes source profiles after a successful merge
   ```

   If `merge` warns that profiles span more than 7 days, delete stale files from `tmp/pprof/production/` and collect fresh ones before merging.

4. **Verify** the merged profiles:

   ```bash
   direnv exec . go tool pprof -top cmd/controller/default.pgo | head -20
   direnv exec . go tool pprof -top cmd/router/default.pgo | head -20
   ```

   Check that `Type: cpu`, Duration is roughly `--seconds x pod count`, and the top functions include application or library code (not only `runtime.*`).

5. **Commit** the updated `cmd/*/default.pgo` files — Go automatically uses `default.pgo` when present.

## Good to know

- **Stale profiles are safe.** The compiler falls back to default optimization for functions not covered by the profile, so stale profiles won't make things slower — the benefit just decreases over time as hot paths drift. Refresh after major refactors.
- **Iterative stability.** It's safe to collect profiles from PGO-optimized binaries and use them for the next build. Go's PGO converges, so there's no need for a two-stage canary process.
- **Cross-platform.** Profiles collected from Linux production pods work for builds targeting any OS or architecture. The Dockerfile builds multi-arch images (amd64/arm64), and both benefit from the same profiles.

See `dev profile --help` and `dev profile <command> --help` for all available flags.
