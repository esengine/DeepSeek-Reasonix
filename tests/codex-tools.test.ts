import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { type ConfirmationChoice, PauseGate } from "../src/core/pause-gate.js";
import { ToolRegistry } from "../src/tools.js";
import {
  type CodexInvocation,
  type CodexRunOptions,
  type CodexRunResult,
  buildCodexDelegateInvocation,
  buildCodexReviewInvocation,
  registerCodexTools,
} from "../src/tools/codex.js";

class AutoGate extends PauseGate {
  constructor(private readonly choice: ConfirmationChoice) {
    super();
  }

  override ask = ((_opts: Parameters<PauseGate["ask"]>[0]) => {
    return Promise.resolve(this.choice);
  }) as PauseGate["ask"];
}

describe("codex tools", () => {
  let tmpRoot: string;

  beforeEach(() => {
    tmpRoot = mkdtempSync(join(tmpdir(), "reasonix-codex-tools-"));
  });

  afterEach(() => {
    rmSync(tmpRoot, { recursive: true, force: true });
  });

  it("builds native review args for the current uncommitted diff by default", () => {
    expect(buildCodexReviewInvocation()).toEqual({
      args: ["review", "--uncommitted"],
      stdin: undefined,
    });
  });

  it("builds focused branch review with global model config before the subcommand", () => {
    expect(
      buildCodexReviewInvocation({
        scope: "branch",
        base: "origin/main",
        instructions: "focus on rollback risk",
        model: "gpt-5.4-mini",
        effort: "high",
      }),
    ).toEqual({
      args: [
        "--model",
        "gpt-5.4-mini",
        "--config",
        'model_reasoning_effort="high"',
        "review",
        "--base",
        "origin/main",
        "-",
      ],
      stdin: "focus on rollback risk",
    });
  });

  it("builds read-only Codex delegation by default", () => {
    expect(
      buildCodexDelegateInvocation({ prompt: "investigate the failing test" }, tmpRoot),
    ).toEqual({
      args: [
        "exec",
        "--skip-git-repo-check",
        "--color",
        "never",
        "-C",
        tmpRoot,
        "--sandbox",
        "read-only",
        "--ask-for-approval",
        "never",
        "-",
      ],
      stdin: "investigate the failing test",
    });
  });

  it("runs codex_review through the injected runner", async () => {
    const calls: Array<{ invocation: CodexInvocation; opts: CodexRunOptions }> = [];
    const runner = async (
      invocation: CodexInvocation,
      opts: CodexRunOptions,
    ): Promise<CodexRunResult> => {
      calls.push({ invocation, opts });
      return { exitCode: 0, output: "no findings", timedOut: false, aborted: false };
    };
    const reg = new ToolRegistry();
    registerCodexTools(reg, { rootDir: tmpRoot, runner });

    const output = await reg.dispatch("codex_review", JSON.stringify({ base: "main" }));

    expect(output).toContain("Codex review");
    expect(output).toContain("no findings");
    expect(calls).toHaveLength(1);
    expect(calls[0]!.invocation.args).toEqual(["review", "--base", "main"]);
    expect(calls[0]!.opts.cwd).toBe(tmpRoot);
  });

  it("blocks write-enabled delegation when the user denies confirmation", async () => {
    let called = false;
    const reg = new ToolRegistry();
    registerCodexTools(reg, {
      rootDir: tmpRoot,
      runner: async () => {
        called = true;
        return { exitCode: 0, output: "should not run", timedOut: false, aborted: false };
      },
    });

    const output = await reg.dispatch(
      "codex_delegate",
      JSON.stringify({ prompt: "fix it", write: true }),
      { confirmationGate: new AutoGate({ type: "deny" }) },
    );

    expect(JSON.parse(output).error).toMatch(/user denied Codex workspace-write delegation/);
    expect(called).toBe(false);
  });

  it("allows read-only delegation in plan mode but refuses write delegation", async () => {
    const reg = new ToolRegistry();
    const calls: CodexInvocation[] = [];
    registerCodexTools(reg, {
      rootDir: tmpRoot,
      runner: async (invocation) => {
        calls.push(invocation);
        return { exitCode: 0, output: "answer", timedOut: false, aborted: false };
      },
    });
    reg.setPlanMode(true);

    const readOnly = await reg.dispatch(
      "codex_delegate",
      JSON.stringify({ prompt: "look for risks" }),
    );
    const write = await reg.dispatch(
      "codex_delegate",
      JSON.stringify({ prompt: "fix it", write: true }),
    );

    expect(readOnly).toContain("answer");
    expect(JSON.parse(write).rejectedReason).toBe("plan-mode");
    expect(calls).toHaveLength(1);
  });
});
