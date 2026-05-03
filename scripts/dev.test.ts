import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProcessPromise } from "zx";

// -- Hoisted mock state (available to vi.mock factories) --

const mocks = vi.hoisted(() => {
  type WatchCallback = (event: string, filename: string | null) => void;

  const state = {
    watchCallbacks: [] as WatchCallback[],
    watcherClosed: false,
  };

  const mockWatcher = {
    close: vi.fn(() => {
      state.watcherClosed = true;
    }),
  };

  const mockWatch = vi.fn((_path: string, _opts: { recursive: boolean }, cb: WatchCallback) => {
    state.watchCallbacks.push(cb);
    return mockWatcher;
  });

  return { state, mockWatch, mockWatcher };
});

// -- Module mocks --

vi.mock("node:fs", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return { ...actual, watch: mocks.mockWatch };
});

// -- Import module under test --

import { createGoWatcher, watchAndRun, type GoWatcher } from "./_watch.ts";

// -- Helpers --

function triggerFileChange(filename: string | null): void {
  for (const cb of mocks.state.watchCallbacks) {
    cb("change", filename);
  }
}

interface MockProc {
  kill: ReturnType<typeof vi.fn>;
  exitCode: Promise<number> & { catch: ReturnType<typeof vi.fn> };
}

function createMockProc(): MockProc {
  const exitCodePromise = Promise.resolve(0);
  return {
    kill: vi.fn(),
    exitCode: Object.assign(exitCodePromise, {
      catch: vi.fn(() => exitCodePromise),
    }),
  };
}

// -- Tests --

beforeEach(() => {
  mocks.state.watchCallbacks.length = 0;
  mocks.state.watcherClosed = false;
  vi.clearAllMocks();
});

// ---- createGoWatcher ----

describe("createGoWatcher", () => {
  it("notifies subscribers when a .go file changes", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange("internal/controller/main.go");
    vi.advanceTimersByTime(200);

    expect(subscriber).toHaveBeenCalledOnce();
    ac.abort();
    vi.useRealTimers();
  });

  it("ignores non-.go files", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange("scripts/dev.ts");
    triggerFileChange("internal/web/static/css/output.css");
    triggerFileChange("package.json");
    triggerFileChange("README.md");
    vi.advanceTimersByTime(200);

    expect(subscriber).not.toHaveBeenCalled();
    ac.abort();
    vi.useRealTimers();
  });

  it("ignores _test.go files", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange("internal/controller/main_test.go");
    vi.advanceTimersByTime(200);

    expect(subscriber).not.toHaveBeenCalled();
    ac.abort();
    vi.useRealTimers();
  });

  it("ignores files in tmp/ directory", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange("tmp/generated.go");
    vi.advanceTimersByTime(200);

    expect(subscriber).not.toHaveBeenCalled();
    ac.abort();
    vi.useRealTimers();
  });

  it("ignores null filenames", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange(null);
    vi.advanceTimersByTime(200);

    expect(subscriber).not.toHaveBeenCalled();
    ac.abort();
    vi.useRealTimers();
  });

  it("debounces rapid changes (only fires once after 200ms)", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    triggerFileChange("internal/a.go");
    vi.advanceTimersByTime(50);
    triggerFileChange("internal/b.go");
    vi.advanceTimersByTime(50);
    triggerFileChange("internal/c.go");
    vi.advanceTimersByTime(200);

    expect(subscriber).toHaveBeenCalledOnce();
    ac.abort();
    vi.useRealTimers();
  });

  it("supports multiple subscribers", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const sub1 = vi.fn();
    const sub2 = vi.fn();
    const sub3 = vi.fn();
    watcher.subscribe(sub1);
    watcher.subscribe(sub2);
    watcher.subscribe(sub3);

    triggerFileChange("internal/handler.go");
    vi.advanceTimersByTime(200);

    expect(sub1).toHaveBeenCalledOnce();
    expect(sub2).toHaveBeenCalledOnce();
    expect(sub3).toHaveBeenCalledOnce();
    ac.abort();
    vi.useRealTimers();
  });

  it("cleans up watcher on abort signal", () => {
    const ac = new AbortController();
    createGoWatcher("/workspace", ac.signal);

    ac.abort();

    expect(mocks.mockWatcher.close).toHaveBeenCalledOnce();
  });

  it("stops notifying after abort", () => {
    vi.useFakeTimers();
    const ac = new AbortController();
    const watcher = createGoWatcher("/workspace", ac.signal);
    const subscriber = vi.fn();
    watcher.subscribe(subscriber);

    // Trigger a change, then abort before debounce fires
    triggerFileChange("internal/handler.go");
    ac.abort();
    vi.advanceTimersByTime(200);

    expect(subscriber).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("passes dir and recursive option to fs.watch", () => {
    const ac = new AbortController();
    createGoWatcher("/my/project", ac.signal);

    expect(mocks.mockWatch).toHaveBeenCalledWith("/my/project", { recursive: true }, expect.any(Function));
    ac.abort();
  });
});

// ---- watchAndRun ----

describe("watchAndRun", () => {
  it("calls spawn immediately on creation", () => {
    const ac = new AbortController();
    const mockProc = createMockProc();
    const spawn = vi.fn(() => mockProc as unknown as ProcessPromise);
    const watcher: GoWatcher = { subscribe: vi.fn() };

    void watchAndRun("test", spawn, watcher, ac.signal);

    expect(spawn).toHaveBeenCalledOnce();
    ac.abort();
  });

  it("kills and respawns when watcher notifies", () => {
    const ac = new AbortController();
    const proc1 = createMockProc();
    const proc2 = createMockProc();
    let callCount = 0;
    const spawn = vi.fn(() => {
      callCount++;
      return (callCount === 1 ? proc1 : proc2) as unknown as ProcessPromise;
    });

    let subscriberFn: (() => void) | undefined;
    const watcher: GoWatcher = {
      subscribe(fn: () => void) {
        subscriberFn = fn;
      },
    };

    void watchAndRun("controller", spawn, watcher, ac.signal);

    // Simulate watcher notification
    subscriberFn!();

    expect(proc1.kill).toHaveBeenCalledOnce();
    expect(spawn).toHaveBeenCalledTimes(2);
    ac.abort();
  });

  it("logs restart message with component name", () => {
    const ac = new AbortController();
    const spawn = vi.fn(() => createMockProc() as unknown as ProcessPromise);
    const logSpy = vi.spyOn(console, "log");

    let subscriberFn: (() => void) | undefined;
    const watcher: GoWatcher = {
      subscribe(fn: () => void) {
        subscriberFn = fn;
      },
    };

    void watchAndRun("router", spawn, watcher, ac.signal);
    subscriberFn!();

    expect(logSpy).toHaveBeenCalledWith("\nRestarting router...\n");
    logSpy.mockRestore();
    ac.abort();
  });

  it("does not restart after abort signal", () => {
    const ac = new AbortController();
    const spawn = vi.fn(() => createMockProc() as unknown as ProcessPromise);

    let subscriberFn: (() => void) | undefined;
    const watcher: GoWatcher = {
      subscribe(fn: () => void) {
        subscriberFn = fn;
      },
    };

    void watchAndRun("controller", spawn, watcher, ac.signal);
    ac.abort();

    // Trigger after abort
    subscriberFn!();

    // Only the initial spawn
    expect(spawn).toHaveBeenCalledOnce();
  });

  it("kills process and resolves promise on abort", async () => {
    const ac = new AbortController();
    const mockProc = createMockProc();
    const spawn = vi.fn(() => mockProc as unknown as ProcessPromise);
    const watcher: GoWatcher = { subscribe: vi.fn() };

    const promise = watchAndRun("controller", spawn, watcher, ac.signal);

    ac.abort();

    await promise;

    expect(mockProc.kill).toHaveBeenCalledOnce();
  });

  it("handles multiple restarts in sequence", () => {
    const ac = new AbortController();
    const procs = [createMockProc(), createMockProc(), createMockProc(), createMockProc()];
    let callCount = 0;
    const spawn = vi.fn(() => {
      return procs[callCount++] as unknown as ProcessPromise;
    });

    let subscriberFn: (() => void) | undefined;
    const watcher: GoWatcher = {
      subscribe(fn: () => void) {
        subscriberFn = fn;
      },
    };

    void watchAndRun("controller", spawn, watcher, ac.signal);

    subscriberFn!();
    subscriberFn!();
    subscriberFn!();

    expect(spawn).toHaveBeenCalledTimes(4);
    expect(procs[0]!.kill).toHaveBeenCalledOnce();
    expect(procs[1]!.kill).toHaveBeenCalledOnce();
    expect(procs[2]!.kill).toHaveBeenCalledOnce();
    // Last proc not killed yet (still running)
    expect(procs[3]!.kill).not.toHaveBeenCalled();

    ac.abort();
  });
});
