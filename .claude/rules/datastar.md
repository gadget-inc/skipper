---
paths:
  - "internal/web/templates/**/*.html"
  - "e2e/**/*.ts"
---

# Datastar Template Conventions

Reference: https://data-star.dev/docs.md

## Event Handlers

Datastar RC.8 uses `data-on:eventname` (colon syntax) — NOT `data-on-eventname` (hyphen syntax).

The attribute parser splits on `:` to find `pluginName:key`. With hyphens, the full string (e.g., `on-change`) becomes the plugin name, which matches nothing — the expression is silently ignored.

```html
<!-- Correct -->
data-on:click="..." data-on:change="..." data-on:keyup__debounce.300ms="..."

<!-- Wrong (silently ignored) -->
data-on-click="..." data-on-change="..."
```

### Exception: Interval and Intersect

`data-on-interval` and `data-on-intersect` use hyphens correctly — they are separate registered plugins, not the `on` plugin with an event key.

```html
data-on-interval__duration.5000ms="@get('/sse/functions')"
```

### Modifiers

Append modifiers with `__` after the event name, chained with `.`:

```html
data-on:keyup__debounce.300ms="..." data-on:click__throttle.500ms="..." data-on:click__once="..." data-on:submit__prevent="..."
```

## Signals

Initialize with `data-signals` using object syntax:

```html
data-signals="{fnSearch: '', fnSort: 'name', fnSortDir: 'asc'}"
```

Read or write signals in expressions with `$signalName`:

```html
data-on:click="$count++" data-text="$count"
```

`_`-prefixed signals (e.g., `$_localValue`) are local — not sent to the backend on action requests.

Derived values via `data-computed`:

```html
data-computed-fullName="$firstName + ' ' + $lastName"
```

## Binding

`data-bind` creates two-way binding between a signal and an input element:

```html
<input data-bind="fnSearch" type="text" />
<select data-bind="fnSort">
  ...
</select>
```

Type is preserved — number inputs bind as numbers, checkboxes as booleans.

## Actions

Backend requests use `@method()` syntax:

```html
@get('/sse/functions') @post('/api/items') @put('/api/items/1') @patch('/api/items/1') @delete('/api/items/1')
```

By default, all signals are sent as query params (GET) or JSON body (mutating). Filter with an options object:

```html
@post('/api/search', {include: ['fnSearch', 'fnSort']})
```

## DOM Rendering

- `data-text="$expr"` — set text content
- `data-show="$expr"` — toggle visibility (display none/block)
- `data-class="{'active': $tab === 'home'}"` — conditional classes
- `data-attr-href="$url"` — set any attribute dynamically
- `data-indicator="$isFetching"` — show element while request is in-flight

## Polling and SSE

Poll on an interval:

```html
<div data-on-interval__duration.5000ms="@get('/sse/functions')"></div>
```

Long-lived SSE connection (fires once on mount):

```html
<div data-init="@get('/listen')"></div>
```

The backend must respond with `text/event-stream` and send `event: datastar-merge-fragments` events to update the DOM.

## Expression Syntax

- Semicolons separate statements: `$a = 1; $b = 2`
- `el` refers to the current element; `evt` refers to the triggering event
- Ternary expressions work as expected:

```html
data-attr-value="$severity === 'all' ? '' : $severity"
```
