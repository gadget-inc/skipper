#!/usr/bin/env -S pnpm node --no-warnings --experimental-strip-types
import { existsSync } from "node:fs";
import { mkdir, rename, rm, stat } from "node:fs/promises";
import { basename, dirname } from "node:path";
import process from "node:process";
import { parseArgs } from "node:util";

import { dedent } from "ts-dedent";
import { $, ProcessOutput, chalk, glob } from "zx";

import { abs, rel, unwrap } from "./_utils.ts";

$.cwd = abs();
$.verbose = true;
$.env["SKIPPER_KUBECTL_CONTEXT"] ??= "orbstack";

// -- Shared helpers --

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function shortReason(error: unknown): string {
  const stderr =
    error instanceof ProcessOutput
      ? error.stderr
      : typeof (error as { stderr?: unknown }).stderr === "string"
        ? ((error as { stderr: string }).stderr as string)
        : "";
  if (stderr) {
    const lines = stderr.split("\n").filter((l) => l.trim().length > 0);
    if (lines.length > 0) return lines.at(-1)!;
  }
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

export async function findProfiles(dir: string, pattern: string, regex: RegExp): Promise<string[]> {
  return (await glob(abs(dir, pattern)))
    .filter((filepath) => regex.test(basename(filepath)))
    .sort((a, b) => {
      const aIndex = parseInt(unwrap(basename(a).match(regex)?.[1], "aIndex is undefined"));
      const bIndex = parseInt(unwrap(basename(b).match(regex)?.[1], "bIndex is undefined"));
      return aIndex - bIndex;
    });
}

export async function mergeProfiles(profiles: string[]): Promise<string> {
  if (profiles.length === 1) return profiles[0]!;
  const merged = abs(`tmp/pprof/merged-diff-base-${globalThis.crypto.randomUUID()}.pb.gz`);
  try {
    await $`go tool pprof -proto ${profiles} > ${merged}`;
  } catch (error) {
    await rm(merged, { force: true });
    throw error;
  }
  return merged;
}

// -- Subcommands --

export async function fetchProfile(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      type: { type: "string", default: "heap", short: "t" },
      component: { type: "string", default: "controller", short: "c" },
      production: { type: "boolean", default: false, short: "p" },
      seconds: { type: "string", default: "30", short: "s" },
      help: { type: "boolean", default: false, short: "h" },
      web: { type: "boolean", default: false, short: "w" },
      diff: { type: "boolean", default: false, short: "d" },
      spread: { type: "boolean", default: false },
    },
    allowPositionals: true,
    allowNegative: true,
  });

  if (flags.values.help) {
    console.log(dedent`
      profile fetch [pod] [flags]

        Fetch a pprof profile from a running pod.

      Flags:
        -t, --type=<type>       The type of profile to fetch. (heap, cpu) (default: heap)
        -c, --component=<name>  The component to profile. (controller, router) (default: controller)
        -p, --production        Fetch a profile from production. (default: false)
        -s, --seconds=<n>       CPU profile duration in seconds. (default: 30)
        -w, --web, --no-web     Open the web UI for the profile. (default: false)
        -d, --diff              Compare the profile to previously fetched profiles. (default: false)
            --spread            Fetch one profile from every pod. (default: false)
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  if (flags.values.spread && flags.positionals.length > 0) {
    throw new Error("Cannot use --spread with a positional pod name");
  }
  if (flags.values.spread && flags.values.web) {
    throw new Error("Cannot use --spread with --web");
  }
  if (flags.values.spread && flags.values.diff) {
    throw new Error("Cannot use --spread with --diff");
  }

  const context = flags.values.production ? "gke_gadget-core-production_us-central1_main" : $.env["SKIPPER_KUBECTL_CONTEXT"]!;
  const namespace = flags.values.production ? "skipper-production" : "skipper-development";
  const selector = `app.kubernetes.io/name=skipper,app.kubernetes.io/component=${flags.values.component},app.kubernetes.io/instance=${namespace}`;

  let pods: string[];

  if (flags.values.spread) {
    const jsonpath = '{range .items[*]}{.metadata.name}{"\\n"}{end}';
    let output: string;
    try {
      output =
        await $`kubectl --context=${context} --namespace=${namespace} get pods --selector=${selector} --output=${`jsonpath=${jsonpath}`}`.text();
    } catch (error) {
      throw new Error(`Failed to list pods: ${error instanceof Error ? error.message : String(error)}`);
    }
    pods = output.split("\n").filter((p) => p.length > 0);
    if (pods.length === 0) {
      throw new Error("No pods found");
    }
  } else if (flags.positionals.length > 0) {
    pods = [flags.positionals[0]!];
  } else {
    let pod: string;
    try {
      pod =
        await $`kubectl --context=${context} --namespace=${namespace} get pods --selector=${selector} --output=jsonpath={.items[0].metadata.name}`.text();
    } catch (error) {
      throw new Error(`Failed to find pod: ${error instanceof Error ? error.message : String(error)}`);
    }
    if (!pod) {
      throw new Error("No pod found");
    }
    pods = [pod];
  }

  let endpoint: string, query: string;

  switch (flags.values.type) {
    case "heap":
      endpoint = "heap";
      query = "gc=1";
      break;
    case "cpu": {
      const seconds = parseInt(flags.values.seconds!, 10);
      if (!Number.isFinite(seconds) || seconds <= 0 || String(seconds) !== flags.values.seconds) {
        throw new Error(`Invalid duration: ${flags.values.seconds} (must be a positive integer)`);
      }
      endpoint = "profile";
      query = `seconds=${seconds}`;
      break;
    }
    default:
      throw new Error(`Invalid profile type: ${flags.values.type}`);
  }

  const environment = flags.values.production ? "production" : "development";
  const profileDir = `tmp/pprof/${environment}/${flags.values.component}`;

  async function fetchOnePod(pod: string, progress: string): Promise<{ filename: string; baseProfiles: string[] }> {
    const profileRegex = new RegExp(`${escapeRegExp(pod)}-${flags.values.type}-(\\d+)\\.pb\\.gz`);
    const baseProfiles = await findProfiles(profileDir, `${pod}-${flags.values.type}-*.pb.gz`, profileRegex);

    const index = parseInt(baseProfiles.at(-1)?.match(profileRegex)?.[1] ?? "0") + 1;
    await mkdir(abs(profileDir), { recursive: true });
    const filename = abs(`${profileDir}/${pod}-${flags.values.type}-${index.toString().padStart(3, "0")}.pb.gz`);

    const duration = flags.values.type === "cpu" ? ` (${flags.values.seconds}s)` : "";
    console.log(`${progress}Fetching ${flags.values.type} profile for ${chalk.green(pod)}${duration}`);
    const url = `http://localhost:6060/debug/pprof/${endpoint}?${query}`;
    const tmpFilename = `${filename}.${globalThis.crypto.randomUUID()}.tmp`;
    try {
      await $`kubectl --context=${context} --namespace=${namespace} exec ${pod} -- curl -sf ${url} > ${tmpFilename}`;
      await rename(tmpFilename, filename);
    } catch (error) {
      await rm(tmpFilename, { force: true });
      throw new Error(`Failed to fetch profile from ${pod}: ${shortReason(error)}`);
    }

    console.log();
    console.log(`Profile saved to ${chalk.blue(rel(filename))}`);

    return { filename, baseProfiles };
  }

  if (flags.values.spread) {
    const results = await Promise.allSettled(pods.map((pod, i) => fetchOnePod(pod, `[${i + 1}/${pods.length}] `)));

    let failedPods: string[] = [];
    let successCount = 0;

    for (let i = 0; i < results.length; i++) {
      const result = results[i]!;
      if (result.status === "fulfilled") {
        successCount++;
      } else {
        const pod = pods[i]!;
        failedPods.push(pod);
        console.error(chalk.red(`Failed: ${pod}: ${shortReason(result.reason)}`));
      }
    }

    // Retry failed pods sequentially — concurrent kubectl exec streams often get
    // connection-reset by the API server under contention.
    if (failedPods.length > 0) {
      console.log();
      if (successCount === 0) {
        console.log("All concurrent fetches failed. Retrying sequentially...");
      } else {
        console.log(`Retrying ${failedPods.length} failed pod(s) sequentially...`);
      }
      const stillFailed: string[] = [];
      for (const pod of failedPods) {
        try {
          await fetchOnePod(pod, `[retry] `);
          successCount++;
        } catch (error) {
          stillFailed.push(pod);
          console.error(chalk.red(`Retry failed: ${pod}: ${shortReason(error)}`));
        }
      }
      if (successCount === 0) {
        throw new Error(`All ${pods.length} fetch(es) failed`);
      }
      failedPods = stillFailed;
    }

    if (failedPods.length > 0) {
      const retryFlags = [
        `--type=${flags.values.type}`,
        flags.values.production ? "--production" : "",
        flags.values.component !== "controller" ? `--component=${flags.values.component}` : "",
        flags.values.type === "cpu" && flags.values.seconds !== "30" ? `--seconds=${flags.values.seconds}` : "",
      ]
        .filter(Boolean)
        .join(" ");

      console.log();
      console.log("Retry failed pods:");
      for (const pod of failedPods) {
        console.log(`  profile fetch ${retryFlags} ${pod}`);
      }
    }

    console.log();
    console.log(`Fetched ${successCount}/${pods.length} profile(s). Merge with: ${chalk.dim("profile merge")}`);
    console.log();
    return;
  }

  const { filename: lastFilename, baseProfiles: lastBaseProfiles } = await fetchOnePod(pods[0]!, "");

  if (!flags.values.web && !flags.values.diff) {
    console.log(`Open with: ${chalk.dim(`profile open ${rel(lastFilename)}`)}`);
    if (flags.values.type === "cpu" && !flags.values.production) {
      console.log();
      console.log(chalk.dim("Hint: For PGO, collect CPU profiles from production: profile fetch --type=cpu --production"));
    }
    console.log();
    return;
  }

  const args = [];

  if (flags.values.web) {
    args.push("-http=:");
  }

  let mergedDiffBase: string | undefined;

  if (flags.values.diff && lastBaseProfiles.length > 0) {
    console.log();
    console.log(`Using base profiles:`);
    for (const base of lastBaseProfiles) {
      console.log(`- ${chalk.blue(rel(base))}`);
    }
    mergedDiffBase = await mergeProfiles(lastBaseProfiles);
    args.push("-diff_base", mergedDiffBase);
  }

  console.log();
  try {
    await $`go tool pprof ${args} ${lastFilename}`;
  } finally {
    if (mergedDiffBase && mergedDiffBase !== lastBaseProfiles[0]) {
      await rm(mergedDiffBase, { force: true });
    }
  }
}

export async function open(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      help: { type: "boolean", default: false, short: "h" },
      diff: { type: "boolean", default: false, short: "d" },
      web: { type: "boolean", default: true, short: "w" },
    },
    allowPositionals: true,
    allowNegative: true,
  });

  if (flags.values.help) {
    console.log(dedent`
      profile open <file> [flags]

        Open a saved profile in go tool pprof.

      Flags:
        -w, --web, --no-web   Open the web UI for the profile. (default: true)
        -d, --diff            Compare the profile to previous profiles. (default: false)
        -h, --help            Show this help message.
    `);
    process.exit(0);
  }

  const filepath = flags.positionals[0];
  if (!filepath) {
    throw new Error("No profile provided");
  }

  if (!existsSync(abs(filepath))) {
    throw new Error(`Profile not found: ${filepath}`);
  }

  const profileDir = dirname(filepath);
  const profile = basename(filepath);
  const profilePrefix = profile.replace(/-\d+\.pb\.gz$/, "");
  const profileRegex = new RegExp(`${escapeRegExp(profilePrefix)}-(\\d+)\\.pb\\.gz$`);
  const profileIndex = parseInt(unwrap(profile.match(profileRegex)?.[1], "profile index is undefined"));

  let baseProfiles: string[] = [];

  if (flags.values.diff) {
    const all = await findProfiles(profileDir, `${profilePrefix}-*.pb.gz`, profileRegex);
    baseProfiles = all.filter((p) => {
      const index = parseInt(unwrap(basename(p).match(profileRegex)?.[1], "index is undefined"));
      return index < profileIndex;
    });
  }

  const args = [];

  let mergedDiffBase: string | undefined;

  if (baseProfiles.length > 0) {
    console.log();
    console.log(`Using base profiles:`);
    for (const base of baseProfiles) {
      console.log(`- ${chalk.blue(rel(base))}`);
    }
    mergedDiffBase = await mergeProfiles(baseProfiles);
    args.push("-diff_base", mergedDiffBase);
  }

  if (flags.values.web) {
    args.push("-http=:");
  } else {
    args.push("-top");
  }

  console.log();
  try {
    await $`go tool pprof ${args} ${filepath}`;
  } finally {
    if (mergedDiffBase && mergedDiffBase !== baseProfiles[0]) {
      await rm(mergedDiffBase, { force: true });
    }
  }
}

const STALENESS_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

export async function merge(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      help: { type: "boolean", default: false, short: "h" },
      component: { type: "string", default: "all", short: "c" },
      "dry-run": { type: "boolean", default: false },
      clean: { type: "boolean", default: false },
    },
  });

  if (flags.values.help) {
    console.log(dedent`
      profile merge [flags]

        Merge CPU profiles into default.pgo files for profile-guided optimization.
        Scans tmp/pprof/production/<component>/ for CPU profiles.

      Flags:
        -c, --component=<name>  Component to merge. (controller, router, all) (default: all)
            --dry-run           Show what would be merged without writing files.
            --clean             Delete source profiles after successful merge.
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  const components = flags.values.component === "all" ? ["controller", "router"] : [flags.values.component!];

  let totalProfiles = 0;

  for (const component of components) {
    const profiles = (await glob(abs(`tmp/pprof/production/${component}/*-cpu-*.pb.gz`))).sort();
    totalProfiles += profiles.length;

    if (profiles.length === 0) {
      console.log(`No CPU profiles found for ${chalk.yellow(component)}`);
      continue;
    }

    // Check for staleness: warn if oldest and newest profiles span >7 days
    const mtimes = await Promise.all(profiles.map(async (p) => (await stat(p)).mtimeMs));
    const oldest = Math.min(...mtimes);
    const newest = Math.max(...mtimes);
    if (newest - oldest > STALENESS_THRESHOLD_MS) {
      const days = Math.round((newest - oldest) / (24 * 60 * 60 * 1000));
      console.log(chalk.yellow(`Warning: ${component} profiles span ${days} days — consider using --clean to remove stale profiles`));
    }

    // Check for mixed durations: longer profiles contribute more samples and skew the merge
    const durations = await Promise.all(
      profiles.map(async (p) => {
        const raw = await $`go tool pprof -raw ${p}`.quiet().text();
        const match = raw.match(/Duration:\s*([^,\n]+)/);
        if (!match?.[1]) return null;
        const dur = match[1].trim();
        let seconds = 0;
        for (const [, val, unit] of dur.matchAll(/([\d.]+)(h|m|s)/g)) {
          const n = parseFloat(val!);
          if (unit === "h") seconds += n * 3600;
          else if (unit === "m") seconds += n * 60;
          else seconds += n;
        }
        return seconds > 0 ? seconds : null;
      }),
    );
    const uniqueDurations = [...new Set(durations.filter((d): d is number => d !== null))];
    if (uniqueDurations.length > 1) {
      console.log(
        chalk.yellow(
          `Warning: ${component} profiles have mixed durations (${uniqueDurations.map((d) => `${d}s`).join(", ")}) — longer profiles will be overrepresented in the merge`,
        ),
      );
    }

    const output = abs(`cmd/${component}/default.pgo`);

    console.log(`${chalk.green(component)}: merging ${profiles.length} CPU profile(s)`);
    for (const p of profiles) {
      console.log(`  ${chalk.blue(rel(p))}`);
    }
    console.log(`  → ${chalk.blue(rel(output))}`);

    if (flags.values["dry-run"]) {
      continue;
    }

    await $`go tool pprof -proto ${profiles} > ${output}`;

    if (flags.values.clean) {
      for (const p of profiles) {
        await rm(p);
        console.log(`  ${chalk.dim("deleted")} ${chalk.blue(rel(p))}`);
      }
    }

    console.log();
  }

  if (totalProfiles === 0) {
    console.log("No CPU profiles found in tmp/pprof/production/");
    process.exit(0);
  }
}

export async function analyze(argv: string[]) {
  const flags = parseArgs({
    args: argv,
    options: {
      mode: { type: "string", default: "top" },
      function: { type: "string", short: "f" },
      component: { type: "string", default: "controller", short: "c" },
      nodecount: { type: "string", default: "20", short: "n" },
      cum: { type: "boolean", default: false },
      pgo: { type: "boolean", default: false },
      "diff-base": { type: "string" },
      help: { type: "boolean", default: false, short: "h" },
    },
    allowPositionals: true,
  });

  if (flags.values.help) {
    console.log(dedent`
      profile analyze [file] [flags]

        Analyze a pprof profile and print text output (no interactive UI).

      Modes:
        --mode=top       Ranked hotspot list (default)
        --mode=peek      Callers/callees of a function (requires -f)
        --mode=source    Source-annotated line-level attribution (requires -f)
        --mode=diff      Compare two profiles (requires --diff-base)

      Flags:
        -f, --function=<regex>  Target function regex (required for peek/source)
        -c, --component=<name>  Component for --pgo (controller, router) (default: controller)
        -n, --nodecount=<n>     Number of functions to show (default: 20)
            --cum               Sort by cumulative instead of flat
            --pgo               Analyze committed default.pgo file
            --diff-base=<file>  Base profile for diff mode
        -h, --help              Show this help message.
    `);
    process.exit(0);
  }

  const mode = flags.values.mode!;
  const validModes = ["top", "peek", "source", "diff"];
  if (!validModes.includes(mode)) {
    throw new Error(`Invalid mode: ${mode} (must be one of ${validModes.join(", ")})`);
  }

  if ((mode === "peek" || mode === "source") && !flags.values.function) {
    throw new Error(`--function is required for --mode=${mode}`);
  }

  if (mode === "diff" && !flags.values["diff-base"]) {
    throw new Error("--diff-base is required for --mode=diff");
  }

  let filepath: string;
  if (flags.values.pgo) {
    filepath = abs(`cmd/${flags.values.component}/default.pgo`);
  } else if (flags.positionals.length > 0) {
    filepath = abs(flags.positionals[0]!);
  } else {
    throw new Error("No profile provided (use --pgo or pass a file path)");
  }

  if (!existsSync(filepath)) {
    throw new Error(`Profile not found: ${flags.values.pgo ? `cmd/${flags.values.component}/default.pgo` : flags.positionals[0]}`);
  }

  const args: string[] = [];

  switch (mode) {
    case "top":
      args.push(`-top`, `-nodecount=${flags.values.nodecount}`);
      if (flags.values.cum) args.push("-cum");
      break;
    case "peek":
      args.push(`-peek=${flags.values.function}`);
      break;
    case "source":
      args.push(`-list=${flags.values.function}`);
      break;
    case "diff":
      args.push("-top", `-nodecount=${flags.values.nodecount}`);
      if (flags.values.cum) args.push("-cum");
      args.push(`-diff_base=${abs(flags.values["diff-base"]!)}`);
      break;
  }

  await $`go tool pprof ${args} ${filepath}`;
}

// -- Entry point --

if (import.meta.filename === process.argv[1]) {
  const subcommand = process.argv[2];

  if (!subcommand || subcommand === "--help" || subcommand === "-h") {
    console.log(dedent`
      profile <command> [flags]

        Fetch, open, merge, and analyze pprof profiles.

      Commands:
        fetch [pod] [flags]     Fetch a pprof profile from a running pod
        open <file> [flags]     Open a saved profile in go tool pprof
        merge [flags]           Merge CPU profiles into default.pgo files
        analyze [file] [flags]  Analyze a profile and print text output

      Run 'profile <command> --help' for command-specific flags.
    `);
    process.exit(0);
  }

  switch (subcommand) {
    case "fetch":
      await fetchProfile(process.argv.slice(3));
      break;
    case "open":
      await open(process.argv.slice(3));
      break;
    case "merge":
      await merge(process.argv.slice(3));
      break;
    case "analyze":
      await analyze(process.argv.slice(3));
      break;
    default:
      console.error(`Unknown command: ${subcommand}`);
      console.error(`Run 'profile --help' for usage.`);
      process.exit(1);
  }
}
