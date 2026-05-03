---
title: Web UI
description: Develop and navigate the Skipper web UI.
---

The web UI is a browser-based view of controller state — functions, tenants, instances, events, configuration, and more. During local development it is served at `http://127.0.0.1:8080` by the controller process.

## Running in dev mode

`direnv exec . dev` starts the web UI alongside the controller, router, and docs server. Because the web UI is served by the controller, any command that starts the controller also starts the web UI:

```bash
direnv exec . dev                          # all components (controller, router, docs)
direnv exec . dev --only=controller        # controller only (includes web UI)
direnv exec . dev --only=controller,router # controller + router
```

## Available routes

<div class="table-scroll">

| Path                | Purpose                                                                  |
| ------------------- | ------------------------------------------------------------------------ |
| `/`                 | Dashboard — cluster summary with ready/total instances and recent events |
| `/functions`        | List of all active functions                                             |
| `/functions/{key}`  | Detail view for a single function (instances, scale history)             |
| `/controllers`      | List of all controller peers and ring distribution                       |
| `/controllers/{ip}` | Detail view for a single controller                                      |
| `/routers`          | List of all router peers                                                 |
| `/routers/{ip}`     | Detail view for a single router                                          |
| `/instances/{name}` | Detail view for a single instance                                        |
| `/tenants`          | List of all tenants                                                      |
| `/tenants/{tenant}` | Detail view for a single tenant                                          |
| `/events`           | Event log with function and severity filters                             |
| `/config`           | Current controller configuration                                         |

</div>

Each full-page route also has a corresponding `/sse/*` endpoint that streams live updates via Server-Sent Events (Datastar).

## Template live-reload

When `SKIPPER_WEB_TEMPLATE_DIR` is set (done automatically by `dev`), templates are read from disk on every request instead of using the copies embedded in the binary. This means you can edit any HTML template in `internal/web/templates/` and see the result by refreshing the browser — no restart required.

## Static assets

The stylesheet at `internal/web/static/css/output.css` is hand-authored vanilla CSS — there is no build step. Edit it directly and refresh the browser; in dev mode the controller serves static files from disk. In production the CSS is embedded in the binary at build time.
