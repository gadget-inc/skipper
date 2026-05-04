---
paths:
  - "docs/**"
---

# Docs Writing Rules

## Audience Tracks

Each directory has a fixed audience. Place content in the track whose audience needs it.

- **`guides/`** — Operators deploying Skipper. Answer: "what do I configure / what behavior should I expect?" Flags, tuning knobs, observable states, expected behavior.
- **`architecture/`** — Anyone curious about internals. Answer: "how does it work internally?" Algorithms, data structures, internal protocols, Go package references.
- **`reference/`** — Operators and integration engineers. Answer: "what are the exact options and API surface?" Flag tables, gRPC API, protobuf types.
- **`contributing/`** — Developers working on Skipper. Answer: "how do I develop this?" Dev scripts, build commands, test workflows, profiling.

## Operator Litmus Test

For guide content, ask: "Would an operator deploying Skipper need to know this?" If no, it does not belong in a guide.

These MUST NOT appear in guides:

- Go type names or internal package paths (e.g., `FunctionHash`, `internal/key/`)
- Code-level API signatures (e.g., `log.With(ctx, fields...)`)
- Which Go stdlib types are used internally (e.g., `httputil.ReverseProxy`, `sync.Once`)
- Internal coordination mechanisms (e.g., controller-to-controller forwarding)
- Implementation specifics like informer resync intervals or struct tags

## Cross-linking Policy

- MUST NOT add "See Architecture" or "See Contributing" signpost links from guides or reference pages — these pull operators toward content they don't need
- Cross-links within the same track are fine (e.g., one guide linking to another guide)
- The index page MAY link to any track — it is a navigation hub

## Heading Structure

The page title comes from the frontmatter `title:` field; do NOT repeat it as an H1. Start the body with introductory prose between the frontmatter and the first `##` heading. Section headings begin at `##`.

## Markdown Conventions

Source files are plain Markdown (`.md`) rendered by the in-tree static-site generator at `internal/dev/docssite` (Goldmark-based). Admonitions and table wrappers are shortcodes / vanilla HTML; the renderer does not accept JSX-style component imports.

Wide tables MUST be wrapped:

```html
<div class="table-scroll">

| ... |

</div>
```

Admonitions use the `:::` shortcode syntax:

```md
:::tip
Body text.
:::

:::caution
Body text.
:::

:::note
Body text.
:::
```

Internal links MUST use absolute paths with the `/skipper/` base prefix and trailing slashes: `](/skipper/guides/scaling/)`, not `](guides/scaling)` or `](../scaling)`.
