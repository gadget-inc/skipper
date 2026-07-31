---
title: Watching Logs
description: Stream and filter logs from Skipper pods during development.
---

The `dev logs` command streams logs from Skipper pods running in your local Kubernetes cluster.

```bash
dev logs                           # All pods, current logs
dev logs -f                        # Stream continuously
dev logs -c controller             # Controller only
dev logs -c router --level=warn    # Router warnings and errors
dev logs --grep="trace_id=<id>"    # Filter by trace ID
dev logs --errors --since=5m       # Recent errors
```

See `dev logs --help` for all available flags including output formats and filtering options.

:::note
By default, `dev logs` shows current logs and exits. Use `-f` or `--follow` to stream continuously.
:::

## Local process logs

When running `dev up`, controller and router logs are written to `tmp/logs/controller.jsonl` and `tmp/logs/router.jsonl` as structured JSON. These files are truncated on each restart.

You can also pass `--log-file`, `--log-file-level`, and `--log-file-format` to any command to write logs to a file. When `--log-file` is set, logs go to both stderr and the file. Level and format default to the main `--log-level` and `--log-format`.

## Log format

Skipper emits structured JSON logs by default. Each log line includes contextual fields that accumulate through the call chain. See [Observability](/skipper/guides/observability/) for details on log format, trace correlation, and the available Prometheus metrics.
