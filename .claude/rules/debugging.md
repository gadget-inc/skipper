# Debugging

Scoped to: investigating runtime behavior, test failures, request routing issues

## Log Files

When `dev up` is running, structured JSON logs are written to:

- `tmp/logs/controller.jsonl` — controller logs
- `tmp/logs/router.jsonl` — router logs

Read them to debug runtime issues without scrolling through terminal output.

## Log File Flags

Any command accepts these flags (also configurable via env vars):

- `--log-file <path>` / `SKIPPER_LOG_FILE` — write logs to both stderr and a file
- `--log-file-level` / `SKIPPER_LOG_FILE_LEVEL` — file log level (defaults to `--log-level`)
- `--log-file-format` / `SKIPPER_LOG_FILE_FORMAT` — file log format, `json` or `text` (defaults to `--log-format`)

## Debugging Integration Test Failures

Router integration tests send requests to the local router at `http://127.0.0.1:8081` (override with `SKIPPER_ROUTER_URL`). These require `dev up` to be running with at least the controller and router, and fixtures deployed (`dev deploy --only=fixtures`).

When tests fail, check `tmp/logs/router.jsonl` and `tmp/logs/controller.jsonl` for error context.
