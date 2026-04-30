import { join } from "node:path";
import process from "node:process";

import { beforeEach, describe, expect, it, vi } from "vitest";

// -- Hoisted mock state (available to vi.mock factories) --

const mocks = vi.hoisted(() => {
  const state = {
    isCI: false,
    shellCalls: [] as { strings: TemplateStringsArray; values: unknown[] }[],
    nothrowCalls: [] as { strings: TemplateStringsArray; values: unknown[] }[],
  };

  const mockShell = Object.assign(
    (strings: TemplateStringsArray, ...values: unknown[]) => {
      state.shellCalls.push({ strings, values });
      return Promise.resolve({ stdout: "", exitCode: 0 });
    },
    {
      cwd: "",
      verbose: false,
      stdio: "inherit" as const,
      env: {} as Record<string, string | undefined>,
    },
  );

  const mockNothrow = (strings: TemplateStringsArray, ...values: unknown[]) => {
    state.nothrowCalls.push({ strings, values });
    return Promise.resolve();
  };

  const mockMkdir = vi.fn();

  return { state, mockShell, mockNothrow, mockMkdir };
});

// -- Module mocks --

vi.mock("zx", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return { ...actual, $: mocks.mockShell };
});

vi.mock("./_utils.ts", () => ({
  $nothrow: mocks.mockNothrow,
  abs: (...segments: string[]) => join("/workspace", ...segments),
  get isCI() {
    return mocks.state.isCI;
  },
}));

vi.mock("node:fs/promises", async (importOriginal: () => Promise<Record<string, unknown>>) => {
  const actual = await importOriginal();
  return { ...actual, mkdir: mocks.mockMkdir };
});

// -- Import module under test --

import { printUsage, runAll, runDocs, runGo, runScripts } from "./tests.ts";

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
  mocks.state.nothrowCalls.length = 0;
  mocks.state.isCI = false;
  process.exitCode = undefined;
  vi.clearAllMocks();
});

// ---- printUsage ----

describe("printUsage", () => {
  it("prints usage and exits with code 1", () => {
    const spy = vi.spyOn(console, "log").mockImplementation(() => {});

    printUsage();

    expect(process.exitCode).toBe(1);
    const output = spy.mock.calls.map((args) => args.join(" ")).join("\n");
    expect(output).toContain("tests <subcommand>");
    spy.mockRestore();
  });
});

// ---- runGo — defaults ----

describe("runGo", () => {
  it("defaults to ./... when no path provided", async () => {
    await runGo([]);

    expect(mocks.state.nothrowCalls.length).toBeGreaterThanOrEqual(1);
    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("./...");
  });

  it("preserves explicit path", async () => {
    await runGo(["./internal/controller/..."]);

    expect(mocks.state.nothrowCalls.length).toBeGreaterThanOrEqual(1);
    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("./internal/controller/...");
    // Should not have ./... as a separate default argument
    const allArgs = cmd.replace("./internal/controller/...", "");
    expect(allArgs).not.toContain("./...");
  });

  // ---- runGo — CI behavior ----

  it("adds -count=1 and -race in CI", async () => {
    mocks.state.isCI = true;

    await runGo([]);

    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("-count=1");
    expect(cmd).toContain("-race");
  });

  it("respects existing -count flag in CI", async () => {
    mocks.state.isCI = true;

    await runGo(["-count=5"]);

    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("-count=5");
    expect(cmd).not.toContain("-count=1");
  });

  it("respects existing -race flag in CI", async () => {
    mocks.state.isCI = true;

    await runGo(["-race"]);

    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    const raceMatches = cmd.match(/-race/g);
    expect(raceMatches).toHaveLength(1);
  });

  it("runs allocation tests in CI", async () => {
    mocks.state.isCI = true;

    await runGo([]);

    expect(mocks.state.nothrowCalls).toHaveLength(2);
    const secondCmd = shellCommand(mocks.state.nothrowCalls[1]!);
    expect(secondCmd).toContain("-run=Allocations");
  });

  it("hides skipped summary outside CI", async () => {
    mocks.state.isCI = false;

    await runGo([]);

    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("--hide-summary=skipped");
  });

  it("does not hide skipped summary in CI", async () => {
    mocks.state.isCI = true;

    await runGo([]);

    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).not.toContain("--hide-summary=skipped");
  });
});

// ---- runDocs / runScripts ----

describe("runDocs", () => {
  it("passes args through to pnpm filter", async () => {
    await runDocs(["--watch"]);

    expect(mocks.state.nothrowCalls.length).toBeGreaterThanOrEqual(1);
    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("--filter docs test");
    expect(cmd).toContain("--watch");
  });
});

describe("runScripts", () => {
  it("passes args through to pnpm filter", async () => {
    await runScripts(["--watch"]);

    expect(mocks.state.nothrowCalls.length).toBeGreaterThanOrEqual(1);
    const cmd = shellCommand(mocks.state.nothrowCalls[0]!);
    expect(cmd).toContain("--filter scripts test");
    expect(cmd).toContain("--watch");
  });
});

// ---- runAll ----

describe("runAll", () => {
  it("runs go, docs, and scripts but not e2e", async () => {
    await runAll();

    expect(mocks.state.nothrowCalls.length).toBeGreaterThanOrEqual(3);
    const commands = mocks.state.nothrowCalls.map((call) => shellCommand(call));
    const allCommands = commands.join("\n");
    // Should include gotestsum (Go tests), docs filter, and scripts filter
    expect(allCommands).toContain("gotestsum");
    expect(allCommands).toContain("--filter docs test");
    expect(allCommands).toContain("--filter scripts test");
    // Should not include e2e
    expect(allCommands).not.toContain("--filter e2e");
  });
});
