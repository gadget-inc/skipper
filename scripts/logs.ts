#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import process from "node:process";
import { parseArgs } from "node:util";
import { dedent } from "ts-dedent";
import { $ } from "zx";
import { abs, isCI } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.stdio = "inherit";
$.env["SKIPPER_KUBECTL_CONTEXT"] ??= "orbstack";

const DEFAULT_NAMESPACES =
  "skipper-development,skipper-development-fixtures,skipper-test,skipper-test-fixtures";

const COMPONENT_PODS: Record<string, string> = {
  controller: "skipper-controller",
  router: "skipper-router",
  fixtures: "skipper-fixtures",
  all: ".",
};

const PRESETS: Record<string, Record<string, string | boolean>> = {
  "debug-controller": {
    component: "controller",
    level: "debug",
    since: "10m",
  },
  "debug-router": {
    component: "router",
    level: "debug",
    since: "10m",
  },
  "recent-errors": {
    level: "error",
    since: "30m",
  },
  "test-run": {
    namespace: "skipper-test,skipper-test-fixtures",
    since: "5m",
  },
};

const flags = parseArgs({
  args: process.argv.slice(2),
  allowNegative: true,
  tokens: true,
  options: {
    // Core options
    namespace: {
      type: "string",
      default: DEFAULT_NAMESPACES,
      short: "n",
    },
    file: {
      type: "string",
      default: abs("tmp/logs/logs.log"),
      // No short flag - -f is now used for --follow
    },
    help: {
      type: "boolean",
      default: false,
      short: "h",
    },

    // Component filtering
    component: {
      type: "string",
      default: "all",
      short: "c",
    },

    // Level filtering
    level: {
      type: "string",
      short: "l",
    },
    errors: {
      type: "boolean",
      default: false,
    },

    // Time-based filtering
    since: {
      type: "string",
      short: "s",
    },

    // Streaming control
    follow: {
      type: "boolean",
      default: false,
      short: "f",
    },
    tail: {
      type: "string",
      short: "t",
    },

    // Search/grep
    grep: {
      type: "string",
      short: "g",
    },
    exclude: {
      type: "string",
      short: "E",
    },

    // Output format
    output: {
      type: "string",
      default: "text",
      short: "o",
    },

    // Presets
    preset: {
      type: "string",
      short: "p",
    },

    // Disable file output
    stdout: {
      type: "boolean",
      default: false,
    },
  },
});

if (flags.values.help) {
  console.log(dedent`
    Usage:
      logs [flags]

    Stream or view logs from Skipper pods using stern.

    Flags:
      -n, --namespace <string>   Namespaces to stream from (${DEFAULT_NAMESPACES})
          --file <string>        Output file path (tmp/logs/logs.log)
          --stdout               Disable file output (stdout only)
      -h, --help                 Show this help message

    Component Filtering:
      -c, --component <string>   Filter by component: controller, router, fixtures, all (all)

    Level Filtering:
      -l, --level <string>       Filter by log level: trace, debug, info, warn, error
          --errors               Shortcut for --level=error

    Time-Based Filtering:
      -s, --since <string>       Only show logs newer than duration (e.g., 5m, 1h, 30m)

    Streaming Control:
      -f, --follow               Stream logs continuously (like tail -f)
      -t, --tail <string>        Show only the last N lines (default: 1000)

    Search/Grep:
      -g, --grep <string>        Filter logs matching a pattern (regex)
      -E, --exclude <string>     Exclude logs matching a pattern (regex)

    Output Format:
      -o, --output <string>      Output format: text, json, raw (text)

    Presets:
      -p, --preset <string>      Use a built-in configuration:
                                   debug-controller  Controller logs at debug level, last 10m
                                   debug-router      Router logs at debug level, last 10m
                                   recent-errors     All error logs from last 30m
                                   test-run          Test namespace logs from last 5m

    Examples:
      logs                                    # Show recent logs and exit
      logs -f                                 # Stream logs continuously (follow)
      logs -c controller                      # Show controller logs
      logs --errors --since=5m                # Recent errors only
      logs -g "trace_id=abc123"               # Filter by trace ID
      logs -p recent-errors                   # Use recent-errors preset
      logs -o json > logs.json                # Export as JSON
  `);
  process.exit(0);
}

// Track which flags were explicitly provided by the user
const explicitFlags = new Set(
  flags.tokens?.filter((t) => t.kind === "option").map((t) => (t as { name: string }).name) ?? [],
);

// Apply preset if specified
if (flags.values.preset) {
  const preset = PRESETS[flags.values.preset];
  if (!preset) {
    console.error(
      `Unknown preset: ${flags.values.preset}. Available: ${Object.keys(PRESETS).join(", ")}`,
    );
    process.exit(1);
  }
  // Apply preset values (but don't override explicit flags)
  for (const [key, value] of Object.entries(preset)) {
    // Only apply if user didn't explicitly set this flag
    if (!explicitFlags.has(key)) {
      (flags.values as Record<string, unknown>)[key] = value;
    }
  }
}

// Handle --errors shortcut (only if --level wasn't explicitly set)
if (flags.values.errors && !explicitFlags.has("level")) {
  flags.values.level = "error";
}

// Default tail to 1000 if not following and not explicitly set
if (!flags.values.follow && !flags.values.tail) {
  flags.values.tail = "1000";
}

// Validate component
const component = flags.values.component;
if (!COMPONENT_PODS[component]) {
  console.error(
    `Unknown component: ${component}. Available: ${Object.keys(COMPONENT_PODS).join(", ")}`,
  );
  process.exit(1);
}

// Validate output format
const outputFormat = flags.values.output;
if (!["text", "json", "raw"].includes(outputFormat)) {
  console.error(`Unknown output format: ${outputFormat}. Available: text, json, raw`);
  process.exit(1);
}

// Build stern command arguments
const sternArgs: string[] = [];

// Pod selector pattern
const podPattern = COMPONENT_PODS[component];
sternArgs.push(podPattern);

// Context
sternArgs.push(`--context=${$.env["SKIPPER_KUBECTL_CONTEXT"]}`);

// Namespaces
sternArgs.push(`--namespace=${flags.values.namespace}`);

// Output template based on format
if (outputFormat === "text") {
  sternArgs.push('--template={{ printf "%s/%s: %s\\n" .Namespace .PodName .Message }}');
} else if (outputFormat === "json") {
  sternArgs.push("--output=json");
} // raw = no template, uses stern defaults

// Time-based filtering
if (flags.values.since) {
  sternArgs.push(`--since=${flags.values.since}`);
}

// Tail
if (flags.values.tail) {
  sternArgs.push(`--tail=${flags.values.tail}`);
}

// Exit after current logs unless following
if (!flags.values.follow) {
  sternArgs.push("--no-follow");
}

// Grep include pattern
if (flags.values.grep) {
  sternArgs.push(`--include=${flags.values.grep}`);
}

// Grep exclude pattern
if (flags.values.exclude) {
  sternArgs.push(`--exclude=${flags.values.exclude}`);
}

// Level filtering using stern's include pattern
if (flags.values.level) {
  const level = flags.values.level.toLowerCase();
  const validLevels = ["trace", "debug", "info", "warn", "error"];
  if (!validLevels.includes(level)) {
    console.error(`Unknown level: ${level}. Available: ${validLevels.join(", ")}`);
    process.exit(1);
  }

  // Build regex to match this level and higher (case-insensitive)
  const levelIndex = validLevels.indexOf(level);
  const matchLevels = validLevels.slice(levelIndex);
  // Match level in various formats: level=INFO, "level":"INFO", [INFO], etc.
  // Use (?i) for case-insensitive matching (Go regex syntax used by stern)
  const levelPattern = `(?i)(level[=:]\\s*"?(${matchLevels.join("|")})|(\\[\\s*(${matchLevels.join("|")})\\s*\\]))`;
  sternArgs.push(`--include=${levelPattern}`);
}

// Disable colors in CI
if (isCI) {
  sternArgs.push("--color=never");
}

// Create output directory if needed
if (!flags.values.stdout) {
  await mkdir(dirname(flags.values.file), { recursive: true });
}

// Run stern with or without tee
if (flags.values.stdout) {
  await $`stern ${sternArgs}`;
} else {
  await $`stern ${sternArgs} | tee ${flags.values.file}`;
}
