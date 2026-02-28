import { basename, join } from "node:path";
import process from "node:process";
import { beforeEach, describe, expect, it, vi } from "vitest";

// -- Hoisted mock state (available to vi.mock factories) --

const mocks = vi.hoisted(() => {
  const state = {
    shellCalls: [] as { strings: TemplateStringsArray; values: unknown[] }[],
    shellTextReturn: "",
    globResults: [] as string[],
    shellThrowOnCallNumbers: [] as number[],
    shellErrorStderr: "" as string,
  };

  const mockShell = Object.assign(
    (strings: TemplateStringsArray, ...values: unknown[]) => {
      state.shellCalls.push({ strings, values });
      const callIndex = state.shellCalls.length - 1;
      const shouldThrow = state.shellThrowOnCallNumbers.includes(callIndex);
      const error = Object.assign(new Error("command failed"), {
        stderr: state.shellErrorStderr || "",
      });
      const result = shouldThrow
        ? Promise.reject(error)
        : Promise.resolve({ stdout: state.shellTextReturn, exitCode: 0 });
      // Prevent unhandled rejection when .text() is called instead of direct await
      if (shouldThrow) result.catch(() => {});
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const chainable: any = Object.assign(result, {
        nothrow: () => chainable,
        quiet: () => chainable,
        text: () =>
          shouldThrow
            ? Promise.reject(
                Object.assign(new Error("command failed"), {
                  stderr: state.shellErrorStderr || "",
                }),
              )
            : Promise.resolve(state.shellTextReturn),
      });
      return chainable;
    },
    { cwd: "", verbose: false, env: {} as Record<string, string | undefined> },
  );

  const mockGlob = vi.fn(async (_pattern: string) => [...state.globResults]);
  const mockMkdir = vi.fn();
  const mockExistsSync = vi.fn((_path: string) => true);
  const mockRename = vi.fn();
  const mockRm = vi.fn();
  const mockStat = vi.fn(async (_path: string) => ({ mtimeMs: Date.now() }));

  return { state, mockShell, mockGlob, mockMkdir, mockExistsSync, mockRename, mockRm, mockStat };
});

// -- Module mocks --

vi.mock("zx", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return { ...actual, $: mocks.mockShell, glob: mocks.mockGlob };
});

vi.mock("node:fs", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return { ...actual, existsSync: mocks.mockExistsSync };
});

vi.mock("node:fs/promises", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return {
    ...actual,
    mkdir: mocks.mockMkdir,
    rename: mocks.mockRename,
    rm: mocks.mockRm,
    stat: mocks.mockStat,
  };
});

vi.mock("./_utils.ts", () => ({
  abs: (...segments: string[]) => join("/workspace", ...segments),
  rel: (...segments: string[]) => join(...segments),
  unwrap: <T>(value: T, message?: string): NonNullable<T> => {
    if (value == null) throw new Error(message ?? "unwrap failed");
    return value as NonNullable<T>;
  },
}));

// -- Import module under test --

import { analyze, fetchProfile, findProfiles, merge, mergeProfiles, open } from "./profile.ts";

// -- Helpers --

function shellCommand(call: { strings: TemplateStringsArray; values: unknown[] }): string {
  let result = call.strings[0] ?? "";
  for (let i = 0; i < call.values.length; i++) {
    const value = call.values[i];
    result += Array.isArray(value) ? value.join(" ") : String(value);
    result += call.strings[i + 1] ?? "";
  }
  return result;
}

// -- Tests --

beforeEach(() => {
  mocks.state.shellCalls.length = 0;
  mocks.state.shellTextReturn = "";
  mocks.state.globResults.length = 0;
  mocks.state.shellThrowOnCallNumbers.length = 0;
  mocks.state.shellErrorStderr = "";
  vi.clearAllMocks();
});

// ---- findProfiles ----

describe("findProfiles", () => {
  it("filters files matching the regex and sorts by numeric index", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/controller/my-pod-heap-002.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-010.pb.gz",
      "/workspace/tmp/pprof/controller/unrelated-file.txt",
    ];
    const regex = /my-pod-heap-(\d+)\.pb\.gz/;
    const result = await findProfiles("tmp/pprof/controller", "my-pod-heap-*.pb.gz", regex);
    expect(result).toEqual([
      "/workspace/tmp/pprof/controller/my-pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-002.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-010.pb.gz",
    ]);
  });

  it("sorts numerically, not lexicographically", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/controller/pod-cpu-009.pb.gz",
      "/workspace/tmp/pprof/controller/pod-cpu-010.pb.gz",
      "/workspace/tmp/pprof/controller/pod-cpu-002.pb.gz",
    ];
    const regex = /pod-cpu-(\d+)\.pb\.gz/;
    const result = await findProfiles("tmp/pprof/controller", "pod-cpu-*.pb.gz", regex);
    expect(result).toEqual([
      "/workspace/tmp/pprof/controller/pod-cpu-002.pb.gz",
      "/workspace/tmp/pprof/controller/pod-cpu-009.pb.gz",
      "/workspace/tmp/pprof/controller/pod-cpu-010.pb.gz",
    ]);
  });

  it("returns empty array when glob finds nothing", async () => {
    mocks.state.globResults = [];
    const result = await findProfiles(
      "tmp/pprof/controller",
      "pod-heap-*.pb.gz",
      /pod-heap-(\d+)\.pb\.gz/,
    );
    expect(result).toEqual([]);
  });

  it("works with regex metacharacters in the pattern when escaped", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/controller/pod.name-heap-001.pb.gz",
      "/workspace/tmp/pprof/controller/pod.name-heap-002.pb.gz",
      "/workspace/tmp/pprof/controller/podXname-heap-003.pb.gz",
    ];
    // An escaped regex should NOT match "podXname" (the dot is literal)
    const regex = /pod\.name-heap-(\d+)\.pb\.gz/;
    const result = await findProfiles("tmp/pprof/controller", "pod.name-heap-*.pb.gz", regex);
    expect(result).toEqual([
      "/workspace/tmp/pprof/controller/pod.name-heap-001.pb.gz",
      "/workspace/tmp/pprof/controller/pod.name-heap-002.pb.gz",
    ]);
  });
});

// ---- mergeProfiles ----

describe("mergeProfiles", () => {
  it("returns single profile path directly without shell call", async () => {
    const result = await mergeProfiles(["/workspace/tmp/pprof/single.pb.gz"]);
    expect(result).toBe("/workspace/tmp/pprof/single.pb.gz");
    expect(mocks.state.shellCalls).toHaveLength(0);
  });

  it("calls go tool pprof -proto for multiple profiles", async () => {
    const profiles = [
      "/workspace/tmp/pprof/pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/pod-heap-002.pb.gz",
    ];
    const result = await mergeProfiles(profiles);
    expect(result).toMatch(/^\/workspace\/tmp\/pprof\/merged-diff-base-[\da-f-]+\.pb\.gz$/);
    expect(mocks.state.shellCalls).toHaveLength(1);
    const cmd = shellCommand(mocks.state.shellCalls[0]!);
    expect(cmd).toContain("go tool pprof -proto");
    expect(mocks.state.shellCalls[0]!.values).toContain(profiles);
  });
});

// ---- fetchProfile ----

describe("fetchProfile", () => {
  it("uses production context and namespace with --production", async () => {
    await fetchProfile(["--production", "my-pod"]);

    const execCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("kubectl") && shellCommand(c).includes("exec"),
    );
    expect(execCall).toBeDefined();
    const cmd = shellCommand(execCall!);
    expect(cmd).toContain("--context=gke_gadget-core-production_us-central1_main");
    expect(cmd).toContain("--namespace=skipper-production");
  });

  it("uses orbstack context and development namespace by default", async () => {
    await fetchProfile(["my-pod"]);

    const execCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("kubectl") && shellCommand(c).includes("exec"),
    );
    expect(execCall).toBeDefined();
    const cmd = shellCommand(execCall!);
    expect(cmd).toContain("--context=orbstack");
    expect(cmd).toContain("--namespace=skipper-development");
  });

  it("uses heap endpoint with gc=1 for heap type (default)", async () => {
    await fetchProfile(["my-pod"]);

    const execCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(execCall).toBeDefined();
    const cmd = shellCommand(execCall!);
    expect(cmd).toContain("/debug/pprof/heap?gc=1");
  });

  it("uses profile endpoint with seconds=30 for cpu type", async () => {
    await fetchProfile(["--type=cpu", "my-pod"]);

    const execCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(execCall).toBeDefined();
    const cmd = shellCommand(execCall!);
    expect(cmd).toContain("/debug/pprof/profile?seconds=30");
  });

  it("throws on invalid --type", async () => {
    await expect(fetchProfile(["--type=invalid", "my-pod"])).rejects.toThrow(
      "Invalid profile type: invalid",
    );
  });

  it("uses positional pod name when provided", async () => {
    await fetchProfile(["my-specific-pod"]);

    // No kubectl get pods call — the only kubectl call should be exec
    const kubectlCalls = mocks.state.shellCalls.filter((c) => shellCommand(c).includes("kubectl"));
    expect(kubectlCalls).toHaveLength(1);
    expect(shellCommand(kubectlCalls[0]!)).toContain("exec my-specific-pod");
  });

  it("queries kubectl for pod name when no positional provided", async () => {
    mocks.state.shellTextReturn = "auto-discovered-pod";

    await fetchProfile([]);

    const getPodsCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("get pods"));
    expect(getPodsCall).toBeDefined();

    const execCall = mocks.state.shellCalls.find((c) =>
      shellCommand(c).includes("exec auto-discovered-pod"),
    );
    expect(execCall).toBeDefined();
  });

  it("throws when kubectl returns empty pod name", async () => {
    mocks.state.shellTextReturn = "";
    await expect(fetchProfile([])).rejects.toThrow("No pod found");
  });

  it("saves to development directory by default", async () => {
    await fetchProfile(["my-pod"]);

    const curlCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(curlCall).toBeDefined();
    const cmd = shellCommand(curlCall!);
    expect(cmd).toContain("tmp/pprof/development/controller/my-pod-heap-001.pb.gz");
  });

  it("saves to production directory with --production", async () => {
    await fetchProfile(["--production", "my-pod"]);

    const curlCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(curlCall).toBeDefined();
    const cmd = shellCommand(curlCall!);
    expect(cmd).toContain("tmp/pprof/production/controller/my-pod-heap-001.pb.gz");
  });

  it("computes next index from existing profiles", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/development/controller/my-pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/development/controller/my-pod-heap-002.pb.gz",
      "/workspace/tmp/pprof/development/controller/my-pod-heap-003.pb.gz",
    ];

    await fetchProfile(["my-pod"]);

    // The curl command should write to a file with index 004
    const curlCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(curlCall).toBeDefined();
    const cmd = shellCommand(curlCall!);
    expect(cmd).toContain("my-pod-heap-004.pb.gz");
  });

  it("passes -diff_base when --diff with existing base profiles", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/development/controller/my-pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/development/controller/my-pod-heap-002.pb.gz",
    ];

    await fetchProfile(["--diff", "my-pod"]);

    const pprofCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("go tool pprof") && !shellCommand(c).includes("-proto"),
    );
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-diff_base");
  });

  it("escapes regex metacharacters in pod names", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/development/controller/pod.name-heap-001.pb.gz",
      "/workspace/tmp/pprof/development/controller/podXname-heap-099.pb.gz",
    ];

    await fetchProfile(["pod.name"]);

    // Only pod.name profiles should match, not podXname
    const curlCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(curlCall).toBeDefined();
    const cmd = shellCommand(curlCall!);
    // Index should be 2, because only pod.name-heap-001 matches (not podXname-heap-099)
    expect(cmd).toContain("pod.name-heap-002.pb.gz");
  });

  it("skips go tool pprof entirely by default (--web is false)", async () => {
    await fetchProfile(["my-pod"]);

    const pprofCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("go tool pprof") && !shellCommand(c).includes("-proto"),
    );
    expect(pprofCall).toBeUndefined();
  });

  it("includes -http=: when --web is explicitly passed", async () => {
    await fetchProfile(["--web", "my-pod"]);

    const pprofCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("go tool pprof") && !shellCommand(c).includes("-proto"),
    );
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-http=:");
  });

  it("uses custom --seconds value in CPU profile URL", async () => {
    await fetchProfile(["--type=cpu", "--seconds=60", "my-pod"]);

    const execCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(execCall).toBeDefined();
    const cmd = shellCommand(execCall!);
    expect(cmd).toContain("/debug/pprof/profile?seconds=60");
  });

  it("throws on non-numeric --seconds for CPU type", async () => {
    await expect(fetchProfile(["--type=cpu", "--seconds=abc", "my-pod"])).rejects.toThrow(
      "Invalid duration: abc (must be a positive integer)",
    );
  });

  it("throws on non-positive --seconds for CPU type", async () => {
    await expect(fetchProfile(["--type=cpu", "--seconds=0", "my-pod"])).rejects.toThrow(
      "Invalid duration: 0 (must be a positive integer)",
    );
  });

  it("throws on float --seconds for CPU type", async () => {
    await expect(fetchProfile(["--type=cpu", "--seconds=30.5", "my-pod"])).rejects.toThrow(
      "Invalid duration: 30.5 (must be a positive integer)",
    );
  });

  it("prints PGO hint when fetching CPU profile from local dev", async () => {
    const spy = vi.spyOn(console, "log");

    await fetchProfile(["--type=cpu", "my-pod"]);

    const hintLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Hint:"),
    );
    expect(hintLog).toBeDefined();
    expect(hintLog![0]).toContain("profile fetch --type=cpu --production");
    spy.mockRestore();
  });

  it("does not print PGO hint for CPU profile from production", async () => {
    const spy = vi.spyOn(console, "log");

    await fetchProfile(["--type=cpu", "--production", "my-pod"]);

    const hintLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Hint:"),
    );
    expect(hintLog).toBeUndefined();
    spy.mockRestore();
  });

  it("does not print PGO hint for heap profile", async () => {
    const spy = vi.spyOn(console, "log");

    await fetchProfile(["my-pod"]);

    const hintLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Hint:"),
    );
    expect(hintLog).toBeUndefined();
    spy.mockRestore();
  });

  it("throws descriptive error when kubectl fails to find pod", async () => {
    mocks.state.shellThrowOnCallNumbers = [0];
    await expect(fetchProfile([])).rejects.toThrow("Failed to find pod");
  });

  it("writes to temp file then renames on success", async () => {
    await fetchProfile(["my-pod"]);

    const curlCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("curl"));
    expect(curlCall).toBeDefined();
    const cmd = shellCommand(curlCall!);
    expect(cmd).toMatch(/\.pb\.gz\.[0-9a-f-]+\.tmp/);

    expect(mocks.mockRename).toHaveBeenCalledTimes(1);
    const [tmpPath, finalPath] = mocks.mockRename.mock.calls[0]!;
    expect(tmpPath).toMatch(/\.pb\.gz\.[0-9a-f-]+\.tmp$/);
    expect(finalPath).toMatch(/\.pb\.gz$/);
  });

  it("cleans up temp file when fetch fails", async () => {
    mocks.state.shellThrowOnCallNumbers = [0];

    await expect(fetchProfile(["my-pod"])).rejects.toThrow("Failed to fetch profile from my-pod");

    expect(mocks.mockRm).toHaveBeenCalledTimes(1);
    expect(mocks.mockRm.mock.calls[0]![0]).toMatch(/\.pb\.gz\.[0-9a-f-]+\.tmp$/);
    expect(mocks.mockRm.mock.calls[0]![1]).toEqual({ force: true });
  });
});

// ---- fetchProfile --spread ----

describe("fetchProfile --spread", () => {
  it("lists all pods and fetches from each", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b\npod-c";

    await fetchProfile(["--spread"]);

    const execCalls = mocks.state.shellCalls.filter(
      (c) => shellCommand(c).includes("kubectl") && shellCommand(c).includes("exec"),
    );
    expect(execCalls).toHaveLength(3);
    expect(shellCommand(execCalls[0]!)).toContain("exec pod-a");
    expect(shellCommand(execCalls[1]!)).toContain("exec pod-b");
    expect(shellCommand(execCalls[2]!)).toContain("exec pod-c");
  });

  it("throws when no pods found", async () => {
    mocks.state.shellTextReturn = "";
    await expect(fetchProfile(["--spread"])).rejects.toThrow("No pods found");
  });

  it("throws when combined with a positional pod name", async () => {
    await expect(fetchProfile(["--spread", "my-pod"])).rejects.toThrow(
      "Cannot use --spread with a positional pod name",
    );
  });

  it("throws when combined with --web", async () => {
    await expect(fetchProfile(["--spread", "--web"])).rejects.toThrow(
      "Cannot use --spread with --web",
    );
  });

  it("throws when combined with --diff", async () => {
    await expect(fetchProfile(["--spread", "--diff"])).rejects.toThrow(
      "Cannot use --spread with --diff",
    );
  });

  it("saves each pod to a correctly named file", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";

    await fetchProfile(["--spread"]);

    const curlCalls = mocks.state.shellCalls.filter((c) => shellCommand(c).includes("curl"));
    expect(curlCalls).toHaveLength(2);
    expect(shellCommand(curlCalls[0]!)).toContain("pod-a-heap-001.pb.gz");
    expect(shellCommand(curlCalls[1]!)).toContain("pod-b-heap-001.pb.gz");
  });

  it("uses production context and namespace with --production", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";

    await fetchProfile(["--spread", "--production"]);

    const execCalls = mocks.state.shellCalls.filter(
      (c) => shellCommand(c).includes("kubectl") && shellCommand(c).includes("exec"),
    );
    for (const call of execCalls) {
      const cmd = shellCommand(call);
      expect(cmd).toContain("--context=gke_gadget-core-production_us-central1_main");
      expect(cmd).toContain("--namespace=skipper-production");
    }
  });

  it("prints merge hint", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";
    const spy = vi.spyOn(console, "log");

    await fetchProfile(["--spread"]);

    const hintLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Fetched 2/2 profile(s)"),
    );
    expect(hintLog).toBeDefined();
    expect(hintLog![0]).toContain("profile merge");
    spy.mockRestore();
  });

  it("shows progress indicators for multiple pods", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b\npod-c";
    const spy = vi.spyOn(console, "log");

    await fetchProfile(["--spread"]);

    const progressLogs = spy.mock.calls
      .filter((args) => typeof args[0] === "string" && /\[\d+\/\d+\]/.test(args[0]))
      .map((args) => args[0] as string);
    expect(progressLogs).toHaveLength(3);
    expect(progressLogs[0]).toContain("[1/3]");
    expect(progressLogs[1]).toContain("[2/3]");
    expect(progressLogs[2]).toContain("[3/3]");
    spy.mockRestore();
  });

  it("throws descriptive error when kubectl fails to list pods", async () => {
    mocks.state.shellThrowOnCallNumbers = [0];
    await expect(fetchProfile(["--spread"])).rejects.toThrow("Failed to list pods");
  });

  it("retries failed pods sequentially and counts them as succeeded", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b\npod-c";
    // call 0: list, 1: pod-a, 2: pod-b (fail), 3: pod-c, 4: pod-b retry (succeeds)
    mocks.state.shellThrowOnCallNumbers = [2];
    const logSpy = vi.spyOn(console, "log");

    await fetchProfile(["--spread"]);

    const summaryLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Fetched"),
    );
    expect(summaryLog).toBeDefined();
    expect(summaryLog![0]).toContain("Fetched 3/3 profile(s)");

    const retryingLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Retrying"),
    );
    expect(retryingLog).toBeDefined();
    expect(retryingLog![0]).toContain("1 failed pod(s) sequentially");

    logSpy.mockRestore();
  });

  it("shows retry commands when retry also fails", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b\npod-c";
    // call 0: list, 1: pod-a, 2: pod-b (fail), 3: pod-c, 4: pod-b retry (fail)
    mocks.state.shellThrowOnCallNumbers = [2, 4];
    const logSpy = vi.spyOn(console, "log");
    const errorSpy = vi.spyOn(console, "error");

    await fetchProfile(["--spread"]);

    const summaryLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Fetched"),
    );
    expect(summaryLog).toBeDefined();
    expect(summaryLog![0]).toContain("Fetched 2/3 profile(s)");

    // Should print short "Failed: <pod>: <reason>" format
    const failedLog = errorSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Failed:"),
    );
    expect(failedLog).toBeDefined();
    expect(failedLog![0]).toContain("pod-b");

    // Should print retry command
    const retryLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Retry failed pods:"),
    );
    expect(retryLog).toBeDefined();

    logSpy.mockRestore();
    errorSpy.mockRestore();
  });

  it("prints retry commands for failed pods with correct flags", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b\npod-c";
    // call 0: list, 1: pod-a (fail), 2: pod-b, 3: pod-c (fail), 4: pod-a retry (fail), 5: pod-c retry (fail)
    mocks.state.shellThrowOnCallNumbers = [1, 3, 4, 5];
    const logSpy = vi.spyOn(console, "log");

    await fetchProfile(["--spread", "--type=cpu", "--production", "--seconds=60"]);

    const retryLogs = logSpy.mock.calls
      .filter((args) => typeof args[0] === "string" && args[0].includes("profile fetch"))
      .map((args) => args[0] as string);
    expect(retryLogs.length).toBeGreaterThanOrEqual(2);
    // Each retry command should include the failed pod name and relevant flags
    const retryText = retryLogs.join("\n");
    expect(retryText).toContain("pod-a");
    expect(retryText).toContain("pod-c");
    expect(retryText).toContain("--type=cpu");
    expect(retryText).toContain("--production");
    expect(retryText).toContain("--seconds=60");

    logSpy.mockRestore();
  });

  it("uses short single-line error format from stderr", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";
    mocks.state.shellThrowOnCallNumbers = [2]; // pod-b fails
    mocks.state.shellErrorStderr = "first line\nconnection reset by peer\n";
    const errorSpy = vi.spyOn(console, "error");

    await fetchProfile(["--spread"]);

    const failedLog = errorSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Failed:"),
    );
    expect(failedLog).toBeDefined();
    // Should use the last non-empty line of stderr as the reason
    expect(failedLog![0]).toContain("connection reset by peer");
    // Should NOT contain the full multi-line stderr dump
    expect(failedLog![0]).not.toContain("first line");

    errorSpy.mockRestore();
  });

  it("recovers when all concurrent fetches fail but retries succeed", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";
    // call 0: list, 1: pod-a (fail), 2: pod-b (fail), 3: pod-a retry (ok), 4: pod-b retry (ok)
    mocks.state.shellThrowOnCallNumbers = [1, 2];
    const logSpy = vi.spyOn(console, "log");

    await fetchProfile(["--spread"]);

    const retryingLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("All concurrent fetches failed"),
    );
    expect(retryingLog).toBeDefined();

    const summaryLog = logSpy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Fetched"),
    );
    expect(summaryLog).toBeDefined();
    expect(summaryLog![0]).toContain("Fetched 2/2 profile(s)");

    logSpy.mockRestore();
  });

  it("throws when all spread fetches fail including retries", async () => {
    mocks.state.shellTextReturn = "pod-a\npod-b";
    // call 0: list, 1: pod-a (fail), 2: pod-b (fail), 3: pod-a retry (fail), 4: pod-b retry (fail)
    mocks.state.shellThrowOnCallNumbers = [1, 2, 3, 4];
    await expect(fetchProfile(["--spread"])).rejects.toThrow("All 2 fetch(es) failed");
  });
});

// ---- open ----

describe("open", () => {
  it("throws when no filepath is provided", async () => {
    await expect(open([])).rejects.toThrow("No profile provided");
  });

  it("extracts correct prefix and constructs regex", async () => {
    await open(["tmp/pprof/controller/my-pod-heap-003.pb.gz"]);

    // Should open pprof with the file
    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("tmp/pprof/controller/my-pod-heap-003.pb.gz");
    expect(cmd).toContain("-http=:");
  });

  it("passes -diff_base with earlier profiles when --diff is set", async () => {
    mocks.state.globResults = [
      "/workspace/tmp/pprof/controller/my-pod-heap-001.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-002.pb.gz",
      "/workspace/tmp/pprof/controller/my-pod-heap-003.pb.gz",
    ];

    await open(["--diff", "tmp/pprof/controller/my-pod-heap-003.pb.gz"]);

    // findProfiles should be called to find related profiles
    expect(mocks.mockGlob).toHaveBeenCalled();
    const globArg = mocks.mockGlob.mock.calls[0]![0] as string;
    expect(basename(globArg)).toBe("my-pod-heap-*.pb.gz");

    // The pprof call should include -diff_base
    const pprofCall = mocks.state.shellCalls.find(
      (c) => shellCommand(c).includes("go tool pprof") && !shellCommand(c).includes("-proto"),
    );
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-diff_base");
  });

  it("opens directly without -diff_base when --diff is not set", async () => {
    await open(["tmp/pprof/controller/my-pod-heap-003.pb.gz"]);

    expect(mocks.mockGlob).not.toHaveBeenCalled();

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).not.toContain("-diff_base");
    expect(cmd).toContain("-http=:");
  });

  it("throws when profile file does not exist", async () => {
    mocks.mockExistsSync.mockReturnValueOnce(false);
    await expect(open(["tmp/pprof/controller/missing-heap-001.pb.gz"])).rejects.toThrow(
      "Profile not found: tmp/pprof/controller/missing-heap-001.pb.gz",
    );
  });

  it("passes -top instead of -http=: when --no-web is passed", async () => {
    await open(["--no-web", "tmp/pprof/controller/my-pod-heap-003.pb.gz"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).not.toContain("-http=:");
    expect(cmd).toContain("-top");
  });
});

// ---- merge ----

describe("merge", () => {
  it("merges CPU profiles from production directory", async () => {
    mocks.mockGlob.mockResolvedValueOnce([
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-002.pb.gz",
    ]);

    await merge(["--component=controller"]);

    expect(mocks.mockGlob).toHaveBeenCalledWith(
      "/workspace/tmp/pprof/production/controller/*-cpu-*.pb.gz",
    );
    // 2 duration checks + 1 merge = 3 shell calls
    expect(mocks.state.shellCalls).toHaveLength(3);
    const mergeCall = mocks.state.shellCalls.findLast((c) =>
      shellCommand(c).includes("go tool pprof -proto"),
    )!;
    expect(mergeCall).toBeDefined();
    const profiles = mergeCall.values[0] as string[];
    expect(profiles).toHaveLength(2);
  });

  it("processes both controller and router with --component=all", async () => {
    mocks.mockGlob
      .mockResolvedValueOnce(["/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz"])
      .mockResolvedValueOnce(["/workspace/tmp/pprof/production/router/pod-xyz78-cpu-001.pb.gz"]);

    await merge([]);

    expect(mocks.mockGlob).toHaveBeenCalledTimes(2);
    // 1 duration + 1 merge per component = 4 shell calls
    const mergeCalls = mocks.state.shellCalls.filter((c) =>
      shellCommand(c).includes("go tool pprof -proto"),
    );
    expect(mergeCalls).toHaveLength(2);
  });

  it("does not write files with --dry-run", async () => {
    mocks.mockGlob.mockResolvedValueOnce([
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-002.pb.gz",
    ]);

    await merge(["--dry-run", "--component=controller"]);

    // Duration checks still run, but no merge call
    expect(mocks.state.shellCalls).toHaveLength(2);
    expect(
      mocks.state.shellCalls.every((c) => shellCommand(c).includes("go tool pprof -raw")),
    ).toBe(true);
  });

  it("skips components with no matching profiles", async () => {
    mocks.mockGlob
      .mockResolvedValueOnce(["/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz"])
      .mockResolvedValueOnce([]);

    await merge([]);

    // 1 duration check + 1 merge for controller; router has no profiles
    const mergeCalls = mocks.state.shellCalls.filter((c) =>
      shellCommand(c).includes("go tool pprof -proto"),
    );
    expect(mergeCalls).toHaveLength(1);
  });

  it("works with realistic Kubernetes pod names", async () => {
    mocks.mockGlob.mockResolvedValueOnce([
      "/workspace/tmp/pprof/production/controller/skipper-production-controller-7f9b8c6d5-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/skipper-production-controller-7f9b8c6d5-abc12-cpu-002.pb.gz",
    ]);

    await merge(["--component=controller"]);

    const mergeCall = mocks.state.shellCalls.findLast((c) =>
      shellCommand(c).includes("go tool pprof -proto"),
    )!;
    expect(mergeCall).toBeDefined();
    const profiles = mergeCall.values[0] as string[];
    expect(profiles).toHaveLength(2);
    expect(profiles[0]).toContain("skipper-production-controller-7f9b8c6d5-abc12-cpu-001.pb.gz");
  });

  it("exits when no CPU profiles found", async () => {
    mocks.mockGlob.mockResolvedValueOnce([]).mockResolvedValueOnce([]);
    const exitSpy = vi.spyOn(process, "exit").mockImplementation((() => {}) as () => never);

    await merge([]);

    expect(exitSpy).toHaveBeenCalledWith(0);
    expect(mocks.state.shellCalls).toHaveLength(0);
    exitSpy.mockRestore();
  });

  it("--clean deletes source profiles after successful merge", async () => {
    const profiles = [
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-002.pb.gz",
    ];
    mocks.mockGlob.mockResolvedValueOnce(profiles);

    await merge(["--clean", "--component=controller"]);

    // 2 duration checks + 1 merge = 3 shell calls
    expect(mocks.state.shellCalls).toHaveLength(3);
    expect(mocks.mockRm).toHaveBeenCalledTimes(2);
    expect(mocks.mockRm).toHaveBeenCalledWith(profiles[0]);
    expect(mocks.mockRm).toHaveBeenCalledWith(profiles[1]);
  });

  it("does not delete files without --clean", async () => {
    mocks.mockGlob.mockResolvedValueOnce([
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
    ]);

    await merge(["--component=controller"]);

    expect(mocks.mockRm).not.toHaveBeenCalled();
  });

  it("--clean --dry-run does not delete files", async () => {
    mocks.mockGlob.mockResolvedValueOnce([
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
    ]);

    await merge(["--clean", "--dry-run", "--component=controller"]);

    // Duration checks still run, but no merge or clean
    expect(mocks.state.shellCalls).toHaveLength(1);
    expect(mocks.mockRm).not.toHaveBeenCalled();
  });

  it("prints staleness warning when profiles span >7 days", async () => {
    const profiles = [
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-002.pb.gz",
    ];
    mocks.mockGlob.mockResolvedValueOnce(profiles);

    const now = Date.now();
    const tenDaysAgo = now - 10 * 24 * 60 * 60 * 1000;
    mocks.mockStat
      .mockResolvedValueOnce({ mtimeMs: tenDaysAgo })
      .mockResolvedValueOnce({ mtimeMs: now });

    const spy = vi.spyOn(console, "log");
    await merge(["--component=controller"]);

    const warningLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Warning:"),
    );
    expect(warningLog).toBeDefined();
    expect(warningLog![0]).toContain("profiles span");
    spy.mockRestore();
  });

  it("does not print staleness warning when profiles are within 7 days", async () => {
    const profiles = [
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-001.pb.gz",
      "/workspace/tmp/pprof/production/controller/pod-abc12-cpu-002.pb.gz",
    ];
    mocks.mockGlob.mockResolvedValueOnce(profiles);

    const now = Date.now();
    mocks.mockStat
      .mockResolvedValueOnce({ mtimeMs: now - 2 * 24 * 60 * 60 * 1000 })
      .mockResolvedValueOnce({ mtimeMs: now });

    const spy = vi.spyOn(console, "log");
    await merge(["--component=controller"]);

    const warningLog = spy.mock.calls.find(
      (args) => typeof args[0] === "string" && args[0].includes("Warning:"),
    );
    expect(warningLog).toBeUndefined();
    spy.mockRestore();
  });
});

// ---- analyze ----

describe("analyze", () => {
  it("throws when no file provided and --pgo not set", async () => {
    await expect(analyze([])).rejects.toThrow(
      "No profile provided (use --pgo or pass a file path)",
    );
  });

  it("throws on invalid mode", async () => {
    await expect(analyze(["--mode=invalid", "--pgo"])).rejects.toThrow(
      "Invalid mode: invalid (must be one of top, peek, source, diff)",
    );
  });

  it("throws when peek mode used without --function", async () => {
    await expect(analyze(["--mode=peek", "--pgo"])).rejects.toThrow(
      "--function is required for --mode=peek",
    );
  });

  it("throws when source mode used without --function", async () => {
    await expect(analyze(["--mode=source", "--pgo"])).rejects.toThrow(
      "--function is required for --mode=source",
    );
  });

  it("throws when diff mode used without --diff-base", async () => {
    await expect(analyze(["--mode=diff", "--pgo"])).rejects.toThrow(
      "--diff-base is required for --mode=diff",
    );
  });

  it("resolves --pgo to cmd/controller/default.pgo by default", async () => {
    await analyze(["--pgo"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("cmd/controller/default.pgo");
  });

  it("resolves --pgo with -c router to cmd/router/default.pgo", async () => {
    await analyze(["--pgo", "-c", "router"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("cmd/router/default.pgo");
  });

  it("uses positional file path when no --pgo", async () => {
    await analyze(["tmp/pprof/my-profile.pb.gz"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("tmp/pprof/my-profile.pb.gz");
  });

  it("throws when profile file does not exist", async () => {
    mocks.mockExistsSync.mockReturnValueOnce(false);
    await expect(analyze(["tmp/missing.pb.gz"])).rejects.toThrow(
      "Profile not found: tmp/missing.pb.gz",
    );
  });

  it("throws when --pgo profile does not exist", async () => {
    mocks.mockExistsSync.mockReturnValueOnce(false);
    await expect(analyze(["--pgo"])).rejects.toThrow(
      "Profile not found: cmd/controller/default.pgo",
    );
  });

  it("uses -top and -nodecount=20 in default top mode", async () => {
    await analyze(["--pgo"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-top");
    expect(cmd).toContain("-nodecount=20");
  });

  it("adds -cum flag when --cum is passed", async () => {
    await analyze(["--pgo", "--cum"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-cum");
  });

  it("uses custom --nodecount", async () => {
    await analyze(["--pgo", "--nodecount=50"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-nodecount=50");
  });

  it("uses -peek flag in peek mode", async () => {
    await analyze(["--pgo", "--mode=peek", "-f", "HashRing"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-peek=HashRing");
  });

  it("uses -list flag in source mode", async () => {
    await analyze(["--pgo", "--mode=source", "-f", "Get"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-list=Get");
  });

  it("uses -diff_base flag in diff mode", async () => {
    await analyze(["--mode=diff", "--diff-base=tmp/before.pb.gz", "tmp/after.pb.gz"]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-diff_base=/workspace/tmp/before.pb.gz");
    expect(cmd).toContain("-top");
  });

  it("diff mode respects --cum and --nodecount", async () => {
    await analyze([
      "--mode=diff",
      "--diff-base=tmp/before.pb.gz",
      "--cum",
      "--nodecount=10",
      "tmp/after.pb.gz",
    ]);

    const pprofCall = mocks.state.shellCalls.find((c) => shellCommand(c).includes("go tool pprof"));
    expect(pprofCall).toBeDefined();
    const cmd = shellCommand(pprofCall!);
    expect(cmd).toContain("-cum");
    expect(cmd).toContain("-nodecount=10");
  });
});
