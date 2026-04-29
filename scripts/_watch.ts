import { watch, type FSWatcher } from "node:fs";
import { extname } from "node:path";

import type { ProcessPromise } from "zx";

/** A Go file watcher that notifies subscribers on non-test `.go` file changes with debouncing. */
export type GoWatcher = {
  subscribe(fn: () => void): void;
};

/** Watches for .go file changes with debouncing. */
export function createGoWatcher(dir: string, signal: AbortSignal): GoWatcher {
  const subscribers: Array<() => void> = [];
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;

  const watcher: FSWatcher = watch(dir, { recursive: true }, (_event, filename) => {
    if (!filename) return;
    if (extname(filename) !== ".go") return;
    if (filename.includes("_test.go")) return;
    if (filename.startsWith("tmp/")) return;

    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      if (signal.aborted) return;
      for (const fn of subscribers) fn();
    }, 200);
  });

  signal.addEventListener("abort", () => {
    clearTimeout(debounceTimer);
    watcher.close();
  });

  return {
    subscribe(fn: () => void) {
      subscribers.push(fn);
    },
  };
}

/** Spawn a process that automatically restarts when the watcher notifies. */
export function watchAndRun(name: string, spawn: () => ProcessPromise, watcher: GoWatcher, signal: AbortSignal): Promise<void> {
  let proc = spawn();
  proc.exitCode.catch(() => {});

  watcher.subscribe(() => {
    if (signal.aborted) return;
    console.log(`\nRestarting ${name}...\n`);
    void proc.kill();
    proc = spawn();
    proc.exitCode.catch(() => {});
  });

  return new Promise<void>((resolve) => {
    signal.addEventListener("abort", () => {
      void proc.kill();
      resolve();
    });
  });
}
