import { type ChildProcess, spawn } from "node:child_process";
import { pauseGate } from "../core/pause-gate.js";
import type { ToolRegistry } from "../tools.js";
import { DEFAULT_MAX_OUTPUT_CHARS, killProcessTree, smartDecodeOutput } from "./shell/exec.js";

const DEFAULT_CODEX_TIMEOUT_SEC = 900;
const MAX_CODEX_TIMEOUT_SEC = 3600;
const CODEX_OUTPUT_CHARS = DEFAULT_MAX_OUTPUT_CHARS;
const VALID_EFFORTS = new Set(["none", "minimal", "low", "medium", "high", "xhigh"]);

export interface CodexToolOptions {
  rootDir: string;
  runner?: CodexRunner;
}

export interface CodexInvocation {
  args: string[];
  stdin?: string;
}

export interface CodexRunOptions {
  cwd: string;
  timeoutSec?: number;
  maxOutputChars?: number;
  signal?: AbortSignal;
}

export interface CodexRunResult {
  exitCode: number | null;
  output: string;
  timedOut: boolean;
  aborted: boolean;
  spawnError?: string;
}

export type CodexRunner = (
  invocation: CodexInvocation,
  opts: CodexRunOptions,
) => Promise<CodexRunResult>;

export interface CodexReviewArgs {
  scope?: "uncommitted" | "branch" | "commit";
  base?: string;
  commit?: string;
  instructions?: string;
  model?: string;
  effort?: string;
  timeoutSec?: number;
}

export interface CodexDelegateArgs {
  prompt: string;
  write?: boolean;
  model?: string;
  effort?: string;
  timeoutSec?: number;
}

export function registerCodexTools(registry: ToolRegistry, opts: CodexToolOptions): ToolRegistry {
  const runner = opts.runner ?? runCodexCli;

  registry.register({
    name: "codex_review",
    description:
      "Run the local Codex CLI's native read-only code review against this workspace. Use after meaningful code changes, before finalizing a fix, or when the user asks for Codex review. It reviews uncommitted changes by default; pass base for a branch review or commit for a commit review. This does not modify files.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        scope: {
          type: "string",
          enum: ["uncommitted", "branch", "commit"],
          description:
            "Review target. Default uncommitted. Use branch with base, or commit with commit.",
        },
        base: {
          type: "string",
          description: "Base branch/ref for branch review, for example main or origin/main.",
        },
        commit: {
          type: "string",
          description: "Commit SHA/ref to review when scope is commit.",
        },
        instructions: {
          type: "string",
          description: "Optional review focus. Keep this short; omit for normal Codex review.",
        },
        model: { type: "string", description: "Optional Codex model override." },
        effort: {
          type: "string",
          enum: ["none", "minimal", "low", "medium", "high", "xhigh"],
          description: "Optional Codex reasoning effort override.",
        },
        timeoutSec: {
          type: "integer",
          description: `Timeout in seconds. Default ${DEFAULT_CODEX_TIMEOUT_SEC}, max ${MAX_CODEX_TIMEOUT_SEC}.`,
        },
      },
    },
    fn: async (args: CodexReviewArgs, ctx) => {
      const invocation = buildCodexReviewInvocation(args);
      const result = await runner(invocation, {
        cwd: opts.rootDir,
        timeoutSec: normalizeTimeout(args.timeoutSec),
        maxOutputChars: CODEX_OUTPUT_CHARS,
        signal: ctx?.signal,
      });
      return formatCodexResult("Codex review", invocation, result);
    },
  });

  registry.register({
    name: "codex_delegate",
    description:
      "Ask local Codex to investigate or pressure-test a focused task in this workspace. Default is read-only and is best for second opinions, root-cause analysis, or patch suggestions. Set write=true only when the user explicitly wants Codex to edit files; Reasonix will ask for confirmation before launching workspace-write Codex.",
    readOnlyCheck: (args: CodexDelegateArgs) => args.write !== true,
    parameters: {
      type: "object",
      properties: {
        prompt: {
          type: "string",
          description:
            "Self-contained task for Codex. Include relevant constraints and expected output.",
        },
        write: {
          type: "boolean",
          description: "Allow Codex to modify files with workspace-write sandbox. Default false.",
        },
        model: { type: "string", description: "Optional Codex model override." },
        effort: {
          type: "string",
          enum: ["none", "minimal", "low", "medium", "high", "xhigh"],
          description: "Optional Codex reasoning effort override.",
        },
        timeoutSec: {
          type: "integer",
          description: `Timeout in seconds. Default ${DEFAULT_CODEX_TIMEOUT_SEC}, max ${MAX_CODEX_TIMEOUT_SEC}.`,
        },
      },
      required: ["prompt"],
    },
    fn: async (args: CodexDelegateArgs, ctx) => {
      const invocation = buildCodexDelegateInvocation(args, opts.rootDir);
      if (args.write === true) {
        const gate = ctx?.confirmationGate ?? pauseGate;
        const choice = await gate.ask({
          kind: "run_command",
          payload: {
            command: `codex ${formatCommandArgs(invocation.args)}`,
            cwd: opts.rootDir,
            timeoutSec: normalizeTimeout(args.timeoutSec),
          },
        });
        if (choice.type === "deny") {
          throw new Error(
            `user denied Codex workspace-write delegation${
              choice.denyContext ? ` - ${choice.denyContext}` : ""
            }`,
          );
        }
      }
      const result = await runner(invocation, {
        cwd: opts.rootDir,
        timeoutSec: normalizeTimeout(args.timeoutSec),
        maxOutputChars: CODEX_OUTPUT_CHARS,
        signal: ctx?.signal,
      });
      return formatCodexResult(
        args.write === true ? "Codex delegated edit" : "Codex delegate",
        invocation,
        result,
      );
    },
  });

  return registry;
}

export function buildCodexReviewInvocation(args: CodexReviewArgs = {}): CodexInvocation {
  const argv = codexGlobalArgs(args);
  argv.push("review");

  const scope = args.scope ?? (args.commit ? "commit" : args.base ? "branch" : "uncommitted");
  if (scope === "commit") {
    const commit = normalizedOptional(args.commit);
    if (!commit) throw new Error("codex_review: commit is required when scope=commit");
    argv.push("--commit", commit);
  } else if (scope === "branch") {
    const base = normalizedOptional(args.base);
    if (!base) throw new Error("codex_review: base is required when scope=branch");
    argv.push("--base", base);
  } else {
    argv.push("--uncommitted");
  }

  const instructions = normalizedOptional(args.instructions);
  if (instructions) {
    argv.push("-");
  }

  return { args: argv, stdin: instructions };
}

export function buildCodexDelegateInvocation(
  args: CodexDelegateArgs,
  rootDir: string,
): CodexInvocation {
  const prompt = normalizedOptional(args.prompt);
  if (!prompt) throw new Error("codex_delegate: prompt is required");

  const argv = codexGlobalArgs(args);
  argv.push(
    "exec",
    "--skip-git-repo-check",
    "--color",
    "never",
    "-C",
    rootDir,
    "--sandbox",
    args.write === true ? "workspace-write" : "read-only",
    "--ask-for-approval",
    "never",
    "-",
  );

  return { args: argv, stdin: prompt };
}

export async function runCodexCli(
  invocation: CodexInvocation,
  opts: CodexRunOptions,
): Promise<CodexRunResult> {
  const timeoutSec = normalizeTimeout(opts.timeoutSec);
  const maxChars = opts.maxOutputChars ?? CODEX_OUTPUT_CHARS;
  const timeoutMs = timeoutSec * 1000;
  const chunks: Buffer[] = [];
  let totalBytes = 0;
  let timedOut = false;
  let aborted = false;
  let child: ChildProcess;

  try {
    child = spawn("codex", invocation.args, {
      cwd: opts.cwd,
      shell: false,
      windowsHide: true,
      detached: process.platform !== "win32",
      env: {
        ...process.env,
        NO_COLOR: "1",
        TERM: process.env.TERM ?? "dumb",
      },
      stdio: ["pipe", "pipe", "pipe"],
    });
  } catch (err) {
    return {
      exitCode: null,
      output: "",
      timedOut: false,
      aborted: false,
      spawnError: (err as Error).message,
    };
  }

  return await new Promise<CodexRunResult>((resolve) => {
    const byteCap = maxChars * 2 * 4;
    const kill = () => killProcessTree(child);
    const timer = setTimeout(() => {
      timedOut = true;
      kill();
    }, timeoutMs);
    const onAbort = () => {
      aborted = true;
      kill();
    };

    const onData = (chunk: Buffer | string) => {
      const buffer = typeof chunk === "string" ? Buffer.from(chunk) : chunk;
      if (totalBytes >= byteCap) return;
      const remaining = byteCap - totalBytes;
      if (buffer.length > remaining) {
        chunks.push(buffer.subarray(0, remaining));
        totalBytes = byteCap;
      } else {
        chunks.push(buffer);
        totalBytes += buffer.length;
      }
    };

    if (opts.signal?.aborted) {
      onAbort();
    } else {
      opts.signal?.addEventListener("abort", onAbort, { once: true });
    }

    child.stdout?.on("data", onData);
    child.stderr?.on("data", onData);
    child.on("error", (err) => {
      clearTimeout(timer);
      opts.signal?.removeEventListener("abort", onAbort);
      resolve({
        exitCode: null,
        output: "",
        timedOut,
        aborted,
        spawnError: err.message,
      });
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      opts.signal?.removeEventListener("abort", onAbort);
      const decoded = smartDecodeOutput(Buffer.concat(chunks));
      const output =
        decoded.length > maxChars
          ? `${decoded.slice(0, maxChars)}\n\n[... truncated ${decoded.length - maxChars} chars ...]`
          : decoded;
      resolve({ exitCode: code, output, timedOut, aborted });
    });

    child.stdin?.end(invocation.stdin ?? "");
  });
}

function codexGlobalArgs(args: { model?: string; effort?: string }): string[] {
  const argv: string[] = [];
  const model = normalizedOptional(args.model);
  if (model) argv.push("--model", model);
  const effort = normalizedOptional(args.effort);
  if (effort) {
    if (!VALID_EFFORTS.has(effort)) {
      throw new Error(
        `unsupported Codex reasoning effort "${effort}". Use one of: ${[...VALID_EFFORTS].join(", ")}`,
      );
    }
    argv.push("--config", `model_reasoning_effort=${JSON.stringify(effort)}`);
  }
  return argv;
}

function normalizeTimeout(value: unknown): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return DEFAULT_CODEX_TIMEOUT_SEC;
  return Math.max(1, Math.min(MAX_CODEX_TIMEOUT_SEC, Math.floor(parsed)));
}

function normalizedOptional(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function formatCodexResult(
  title: string,
  invocation: CodexInvocation,
  result: CodexRunResult,
): string {
  const lines = [
    `${title}`,
    `Command: codex ${formatCommandArgs(invocation.args)}`,
    `Exit: ${result.exitCode ?? "spawn-failed"}${result.timedOut ? " (timeout)" : ""}${
      result.aborted ? " (aborted)" : ""
    }`,
  ];
  if (result.spawnError) lines.push(`Spawn error: ${result.spawnError}`);
  const output = result.output.trim();
  lines.push("", output || "(no output)");
  return lines.join("\n");
}

function formatCommandArgs(args: readonly string[]): string {
  return args.map(quoteArg).join(" ");
}

function quoteArg(arg: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(arg)) return arg;
  return JSON.stringify(arg);
}
