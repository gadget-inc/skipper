---
paths:
  - "docs/content/**/*.css"
---

# Docs Styling -- Vanilla CSS / Primer Tokens

The docs site uses hand-authored vanilla CSS at `docs/content/style.css`. There is no preprocessor, no framework, and no CSS layer architecture -- ordinary cascade rules apply. Edit the stylesheet directly; the dev server picks up changes on save.

## Color Principles (Primer)

- Use Primer neutral gray scale (50--950) for surfaces and backgrounds
- Use Primer blue accent scale for links and interactive elements only
- MUST NOT apply accent colors to surfaces or non-interactive backgrounds

## Primer Design System Reference

### Color System

Primer uses semantic/functional color tokens, not raw scales. Identify the role first (foreground? muted? accent?), then pick the corresponding token -- never use raw hex.

Three categories:

- **Foreground** (`--fgColor-*`): `default` (#1f2328 light / #e6edf3 dark), `muted` (#59636e light / #9198a1 dark), `accent` (#0969da light / #4493f8 dark), `disabled` (#818b98)
- **Background** (`--bgColor-*`): `default` (#ffffff light / #0d1117 dark), `muted` (#f6f8fa light / #151b23 dark), `emphasis` (#25292e light / #e6edf3 dark)
- **Border** (`--borderColor-*`): `default` (#d1d9e0 light / #3d444d dark), `muted` (same with transparency)

Semantic roles: `accent` (blue), `danger` (red), `success` (green), `attention` (yellow). Each has `-emphasis` (strong) and `-muted` (subtle) variants.

### Typography

Font stacks:

- Sans: `-apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji"`
- Mono: `ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, Liberation Mono, monospace`

Font weights: light (300), normal (400), medium (500), semibold (600). Primer does NOT use bold (700) -- semibold (600) is the heaviest standard weight.

Font sizes use rem units: body is 0.875rem (14px), code blocks are 0.8125rem (13px).
