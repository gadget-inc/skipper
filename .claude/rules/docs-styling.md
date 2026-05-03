---
paths:
  - "docs/content/**/*.css"
---

# Docs Styling — GitHub Dark Theme (Starlight)

## CSS Layer Architecture

Starlight uses two named layers:

- `@layer starlight.core` — component styles (sidebar, TOC, nav)
- `@layer starlight.components` — plugin styles (Expressive Code)

`docs/src/styles/global.css` declares the layer order: `base, starlight, theme, components, utilities`.

Rules for overriding:

- Unlayered CSS beats all `@layer` rules — avoid it; prefer layered overrides
- Target the same layer as the rule you're overriding
- When overriding sidebar active styles, use `@layer starlight.core` with `!important`

## Vite Dev vs Production Build

Vite injects CSS as individual `<style>` tags, not a single bundle. Astro scoped component styles may load AFTER global stylesheets, so within the same `@layer`, source order is unreliable across environments. Use `!important` within the target layer to guarantee the override wins in both dev and production.

## Starlight Variable System

Starlight uses `--sl-color-*` CSS variables. The Starlight-Tailwind bridge maps `--color-gray-*` and `--color-accent-*` Tailwind scales to `--sl-color-gray-*` / `--sl-color-accent-*`. The `@theme` block owns the slot variables — do not override those.

Override _derived_ variables (e.g., `--sl-color-text-accent`, `--sl-color-bg-accent`) on doubled-specificity selectors to beat Starlight's `props.css` defaults:

```css
:root[data-theme="dark"]:root {
  --sl-color-text-accent: ...;
}
:root[data-theme="light"]:root {
  --sl-color-text-accent: ...;
}
```

## Sidebar Active Item

`SidebarSublist.astro` styles `[aria-current="page"]` in `@layer starlight.core`:

```css
color: var(--sl-color-text-invert);
background-color: var(--sl-color-text-accent);
```

To override, place rules in `@layer starlight.core` with `!important`.

## Color Principles (Primer)

- Use Primer neutral gray scale (50–950) for surfaces and backgrounds — the gray scale in `@theme` is Primer's neutral gray
- Use Primer blue accent scale for links and interactive elements only — the accent scale in `@theme` is Primer's blue
- MUST NOT apply accent colors to surfaces or non-interactive backgrounds

## Primer Design System Reference

### Color System

Primer uses semantic/functional color tokens, not raw scales. Identify the role first (foreground? muted? accent?), then pick the corresponding token — never use raw hex.

Three categories:

- **Foreground** (`--fgColor-*`): `default` (#1f2328 light / #e6edf3 dark), `muted` (#59636e light / #9198a1 dark), `accent` (#0969da light / #4493f8 dark), `disabled` (#818b98)
- **Background** (`--bgColor-*`): `default` (#ffffff light / #0d1117 dark), `muted` (#f6f8fa light / #151b23 dark), `emphasis` (#25292e light / #e6edf3 dark)
- **Border** (`--borderColor-*`): `default` (#d1d9e0 light / #3d444d dark), `muted` (same with transparency)

Semantic roles: `accent` (blue), `danger` (red), `success` (green), `attention` (yellow). Each has `-emphasis` (strong) and `-muted` (subtle) variants.

### Typography

Font stacks (already declared in `@theme`):

- Sans: `-apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji"`
- Mono: `ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, Liberation Mono, monospace`

Font weights: light (300), normal (400), medium (500), semibold (600). Primer does NOT use bold (700) — semibold (600) is the heaviest standard weight.

Font sizes use rem units: body is 0.875rem (14px), code blocks are 0.8125rem (13px).

### Mapping Primer to Starlight

- `--fgColor-default` → text color, controlled by gray scale in `@theme`
- `--fgColor-accent` → `--sl-color-text-accent` (overridden in derived variables)
- `--bgColor-default` → page background, controlled by gray 900/950 in dark
- `--bgColor-muted` → `--sl-color-bg-accent` (overridden for subtle surfaces)
- `--borderColor-default` → `--sl-color-hairline` / `--sl-color-hairline-light`

## Inline Styles in Plugins

When plugin JS sets `element.style.cssText` directly, those styles apply in all modes. Use `color: inherit` instead of hardcoded values like `#fff` — hardcoded colors are the most common source of light/dark mode bugs.

## Component Overrides (Last Resort)

If the CSS cascade is intractable, Starlight supports component overrides via `astro.config` — copy the component into the project and edit directly. Prefer CSS fixes first; component overrides increase maintenance burden.

## Key Files

- `docs/src/styles/global.css` — all theme overrides; edit here first
- `docs/node_modules/@astrojs/starlight/style/props.css` — Starlight variable defaults (reference only, do not edit)
- `docs/node_modules/@astrojs/starlight/components/SidebarSublist.astro` — sidebar styles (reference only)
- `docs/node_modules/@astrojs/starlight-tailwind/tailwind.css` — Tailwind bridge mapping (reference only)
