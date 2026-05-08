/** cwd pinned to root; non-allowlisted commands throw to a UI confirm gate; spawn is `shell: false`, tokenized argv only. */

import * as pathMod from "node:path";
import { addProjectShellAllowed } from "../config.js";
import { pauseGate } from "../core/pause-gate.js";
import type { ToolRegistry } from "../tools.js";
import { JobRegistry } from "./jobs.js";
import {
  DEFAULT_MAX_OUTPUT_CHARS,
  DEFAULT_TIMEOUT_SEC,
  type RunCommandResult,
  runCommand,
} from "./shell/exec.js";
import { isCommandAllowed } from "./shell/parse.js";

export {
  BUILTIN_ALLOWLIST,
  detectShellOperator,
  isAllowed,
  isCommandAllowed,
  isDqEscape,
  tokenizeCommand,
} from "./shell/parse.js";
export type { ResolveExecutableOptions, RunCommandResult } from "./shell/exec.js";
export {
  injectPowerShellUtf8,
  killProcessTree,
  prepareSpawn,
  quoteForCmdExe,
  resolveExecutable,
  runCommand,
  smartDecodeOutput,
  withUtf8Codepage,
} from "./shell/exec.js";

export interface ShellToolsOptions {
  /** Directory to run commands in. Must be an absolute path. */
  rootDir: string;
  /** Seconds before an individual command is killed. Default: 60. */
  timeoutSec?: number;
  maxOutputChars?: number;
  /** Getter form is load-bearing — newly-persisted "always allow" prefixes MUST take effect mid-session. */
  extraAllowed?: readonly string[] | (() => readonly string[]);
  /** Getter form lets `editMode === "yolo"` flip mid-session without re-registering tools. */
  allowAll?: boolean | (() => boolean);
  jobs?: JobRegistry;
}

/** Error thrown by `run_command` when the command isn't allowlisted. */
export class NeedsConfirmationError extends Error {
  readonly command: string;
  constructor(command: string) {
    super(
      `run_command: "${command}" needs the user's approval before it runs. STOP calling tools now — the TUI has already prompted the user to press y (run) or n (deny). Wait for their next message; it will either be the command's output (if they approved) or an instruction to continue without it (if they denied). Don't retry the command or call other shell commands in the meantime.`,
    );
    this.name = "NeedsConfirmationError";
    this.command = command;
  }
}

export function registerShellTools(registry: ToolRegistry, opts: ShellToolsOptions): ToolRegistry {
  const rootDir = pathMod.resolve(opts.rootDir);
  const timeoutSec = opts.timeoutSec ?? DEFAULT_TIMEOUT_SEC;
  const maxOutputChars = opts.maxOutputChars ?? DEFAULT_MAX_OUTPUT_CHARS;
  const jobs = opts.jobs ?? new JobRegistry();
  // Resolved on every dispatch so newly-persisted "always allow"
  // prefixes take effect inside the session that added them, not just
  // on the next launch. Static arrays are wrapped into a constant
  // getter so the call site below is uniform.
  const getExtraAllowed: () => readonly string[] =
    typeof opts.extraAllowed === "function"
      ? opts.extraAllowed
      : (() => {
          const snapshot = opts.extraAllowed ?? [];
          return () => snapshot;
        })();
  // Resolve dynamically so the TUI can flip yolo mode mid-session and
  // have the registry pick it up on the next dispatch. Static booleans
  // are wrapped into a thunk for uniformity.
  const isAllowAll: () => boolean =
    typeof opts.allowAll === "function" ? opts.allowAll : () => opts.allowAll === true;

  registry.register({
    name: "run_command",
    description:
      "Run a shell command in the project root and return its combined stdout+stderr.\n\nConstraints (read these before the first call):\n• Chain operators `|`, `||`, `&&`, `;` ARE supported — parsed natively, no shell invoked, so semantics are identical on Windows / macOS / Linux. Each chain segment is allowlist-checked individually: `git status | grep main` runs if both halves are allowed.\n• File redirects ARE supported: `>` truncate, `>>` append, `<` stdin from file, `2>` / `2>>` stderr to file, `2>&1` merge stderr→stdout, `&>` both to file. Targets resolve relative to the project root. At most one redirect per fd per segment.\n• Background `&`, heredoc `<<`, command substitution `$(…)`, subshells `(…)`, and process substitution `<(…)` are NOT supported. Wrap a literal `&` arg in quotes; for input use a `<` file or the binary's own --input flag.\n• Env-var expansion `$VAR` is NOT performed — `$VAR` is passed as a literal string. Use the binary's own --env flag or substitute the value yourself.\n• `cd` DOES NOT PERSIST between calls — each call spawns a fresh process rooted at the project. `cd` also does not persist within parsed chains like `cd dir && command`. Use a command-native cwd flag instead: `npm --prefix <dir> run <script>`, `npm --prefix <dir> exec -- <bin>`, `git -C <dir> ...`, `cargo -C <dir> ...`, `pytest <dir>/tests`.\n• Glob patterns (`*.ts`) are passed through as literal arguments — no shell expansion. Use `grep -r`, `rg`, `find -name`, etc.\n• Avoid commands with unbounded output (`netstat -ano`, `find /`, etc.) — they waste tokens. Filter at source: `netstat -ano -p TCP`, `find src -name '*.ts'`, `grep -c`, `wc -l`.\n\nCommon read-only inspection and test/lint/typecheck commands run immediately; anything that could mutate state, install dependencies, or touch the network is refused until the user confirms it in the TUI. Prefer this over asking the user to run a command manually — after edits, run the project's tests to verify.",
    // Plan-mode gate: allow allowlisted commands through (git status,
    // cargo check, ls, grep …) so the model can actually investigate
    // during planning. Anything that would otherwise trigger a
    // confirmation prompt is treated as "not read-only" and bounced.
    readOnlyCheck: (args: { command?: unknown }) => {
      if (isAllowAll()) return true;
      const cmd = typeof args?.command === "string" ? args.command.trim() : "";
      if (!cmd) return false;
      return isCommandAllowed(cmd, getExtraAllowed());
    },
    parameters: {
      type: "object",
      properties: {
        command: {
          type: "string",
          description:
            'Full command line. POSIX-ish quoting. Chain operators `|`, `||`, `&&`, `;` and file redirects `>` / `>>` / `<` / `2>` / `2>>` / `2>&1` / `&>` work natively (no shell). Background `&`, heredoc `<<`, env-var expansion `$VAR`, and command substitution `$(…)` are rejected (or passed through as literal in the case of `$VAR`). To pass an operator character as a literal argument (e.g. a regex), wrap it in quotes: `grep "a|b" file.txt`.',
        },
        timeoutSec: {
          type: "integer",
          description: `Override the default ${timeoutSec}s timeout for a single command.`,
        },
      },
      required: ["command"],
    },
    fn: async (args: { command: string; timeoutSec?: number }, ctx) => {
      const cmd = args.command.trim();
      if (!cmd) throw new Error("run_command: empty command");
      if (!isAllowAll() && !isCommandAllowed(cmd, getExtraAllowed())) {
        const gate = ctx?.confirmationGate ?? pauseGate;
        const choice = await gate.ask({ kind: "run_command", payload: { command: cmd } });
        if (choice.type === "deny") {
          throw new Error(
            `user denied: ${cmd}${choice.denyContext ? ` — ${choice.denyContext}` : ""}`,
          );
        }
        if (choice.type === "always_allow") {
          addProjectShellAllowed(rootDir, choice.prefix);
        }
        // "run_once" — fall through and execute
      }
      const effectiveTimeout = Math.max(1, Math.min(600, args.timeoutSec ?? timeoutSec));
      const result = await runCommand(cmd, {
        cwd: rootDir,
        timeoutSec: effectiveTimeout,
        maxOutputChars,
        signal: ctx?.signal,
      });
      return formatCommandResult(cmd, result);
    },
  });

  registry.register({
    name: "run_background",
    description:
      "Spawn a long-running process (dev server, watcher, any command that doesn't naturally exit) and detach. Waits up to `waitSec` seconds for startup (or until the output matches a readiness signal like 'Local:', 'listening on', 'compiled successfully'), then returns the job id + startup preview. The process keeps running; call `job_output` to tail its logs, `stop_job` to kill it, `list_jobs` to see all running jobs.\n\nSame shell constraints as run_command: NO `&&` / `||` / `|` / `;` / `>` / `<` / `2>&1`, `cd` doesn't persist. Dev servers that need a subdirectory: use the tool's own --prefix / --cwd flag. For Vite specifically, `--prefix` on npm only tells npm where package.json is; vite's server root still defaults to process cwd, so pass `vite <project-dir>` or configure via `vite.config.ts` root.\n\nUSE THIS — not `run_command` — for: npm/yarn/pnpm run dev, uvicorn / flask run, go run, cargo watch, tsc --watch, webpack serve, anything with 'dev' / 'serve' / 'watch' in the name.",
    parameters: {
      type: "object",
      properties: {
        command: {
          type: "string",
          description:
            "Full command line. Same quoting rules as run_command (no pipes / redirects / chaining).",
        },
        waitSec: {
          type: "integer",
          description:
            "Max seconds to wait for startup before returning. 0..30, default 3. A ready-signal match short-circuits this.",
        },
      },
      required: ["command"],
    },
    fn: async (args: { command: string; waitSec?: number }, ctx) => {
      const cmd = args.command.trim();
      if (!cmd) throw new Error("run_background: empty command");
      if (!isAllowAll() && !isCommandAllowed(cmd, getExtraAllowed())) {
        const gate = ctx?.confirmationGate ?? pauseGate;
        const choice = await gate.ask({ kind: "run_background", payload: { command: cmd } });
        if (choice.type === "deny") {
          throw new Error(
            `user denied: ${cmd}${choice.denyContext ? ` — ${choice.denyContext}` : ""}`,
          );
        }
        if (choice.type === "always_allow") {
          addProjectShellAllowed(rootDir, choice.prefix);
        }
        // "run_once" — fall through and execute
      }
      const result = await jobs.start(cmd, {
        cwd: rootDir,
        waitSec: args.waitSec,
        signal: ctx?.signal,
      });
      return formatJobStart(result);
    },
  });

  registry.register({
    name: "job_output",
    description:
      "Read the latest output of a background job started with `run_background`. By default returns the tail of the buffer (last 80 lines). Pass `since` (the `byteLength` from a previous call) to stream only new content incrementally. Tells you whether the job is still running, so you can stop polling when it's done.",
    readOnly: true,
    parallelSafe: true,
    stormExempt: true,
    parameters: {
      type: "object",
      properties: {
        jobId: { type: "integer", description: "Job id returned by run_background." },
        since: {
          type: "integer",
          description:
            "Return only output written past this byte offset (for incremental polling).",
        },
        tailLines: {
          type: "integer",
          description: "Cap the returned slice to the last N lines. Default 80, 0 = unlimited.",
        },
      },
      required: ["jobId"],
    },
    fn: async (args: { jobId: number; since?: number; tailLines?: number }) => {
      const out = jobs.read(args.jobId, {
        since: args.since,
        tailLines: args.tailLines ?? 80,
      });
      if (!out) return `job ${args.jobId}: not found (use list_jobs)`;
      return formatJobRead(args.jobId, out);
    },
  });

  registry.register({
    name: "wait_for_job",
    description:
      "Block until a background job exits or produces new output, bounded by `timeoutMs`. Use this instead of polling `job_output` with identical args when you're intentionally waiting for state to change. Returns JSON with `exited`, `exitCode`, and `latestOutput`.",
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        jobId: { type: "integer", description: "Job id returned by run_background." },
        timeoutMs: {
          type: "integer",
          description:
            "Max time to block before returning if nothing changes. Clamped to 0..30000. Default 5000.",
        },
      },
      required: ["jobId"],
    },
    fn: async (args: { jobId: number; timeoutMs?: number }) => {
      const out = await jobs.waitForJob(args.jobId, { timeoutMs: args.timeoutMs });
      if (!out) return `job ${args.jobId}: not found (use list_jobs)`;
      return {
        jobId: args.jobId,
        exited: out.exited,
        exitCode: out.exitCode,
        latestOutput: out.latestOutput,
      };
    },
  });

  registry.register({
    name: "stop_job",
    description:
      "Stop a background job started with `run_background`. SIGTERM first; SIGKILL after a short grace period if it doesn't exit cleanly. Returns the final output + exit code. Safe to call on an already-exited job.",
    parameters: {
      type: "object",
      properties: {
        jobId: { type: "integer" },
      },
      required: ["jobId"],
    },
    fn: async (args: { jobId: number }) => {
      const rec = await jobs.stop(args.jobId);
      if (!rec) return `job ${args.jobId}: not found`;
      return formatJobStop(rec);
    },
  });

  registry.register({
    name: "list_jobs",
    description:
      "List every background job started this session — running and exited — with id, command, pid, status. Use when you've lost track of which job_id corresponds to which process, or to see what's still alive.",
    readOnly: true,
    parallelSafe: true,
    stormExempt: true,
    parameters: { type: "object", properties: {} },
    fn: async () => {
      const all = jobs.list();
      if (all.length === 0) return "(no background jobs started this session)";
      return all.map(formatJobRow).join("\n");
    },
  });

  return registry;
}

function formatJobStart(r: import("./jobs.js").JobStartResult): string {
  const header = r.stillRunning
    ? `[job ${r.jobId} started · pid ${r.pid ?? "?"} · ${r.readyMatched ? "READY signal matched" : "running (no ready signal yet)"}]`
    : r.exitCode !== null
      ? `[job ${r.jobId} exited during startup · exit ${r.exitCode}]`
      : `[job ${r.jobId} failed to start]`;
  return r.preview ? `${header}\n${r.preview}` : header;
}

function formatJobRead(jobId: number, r: import("./jobs.js").JobReadResult): string {
  const status = r.running
    ? `running · pid ${r.pid ?? "?"}`
    : r.exitCode !== null
      ? `exited ${r.exitCode}`
      : r.spawnError
        ? `failed (${r.spawnError})`
        : "stopped";
  const header = `[job ${jobId} · ${status} · byteLength=${r.byteLength}]\n$ ${r.command}`;
  return r.output ? `${header}\n${r.output}` : header;
}

function formatJobStop(r: import("./jobs.js").JobRecord): string {
  const running = r.running
    ? "still running (SIGKILL may be pending)"
    : `exit ${r.exitCode ?? "?"}`;
  const tail = tailLines(r.output, 40);
  const header = `[job ${r.id} stopped · ${running}]\n$ ${r.command}`;
  return tail ? `${header}\n${tail}` : header;
}

function formatJobRow(r: import("./jobs.js").JobRecord): string {
  const age = ((Date.now() - r.startedAt) / 1000).toFixed(1);
  const state = r.running
    ? `running   ·  pid ${r.pid ?? "?"}`
    : r.exitCode !== null
      ? `exit ${r.exitCode}`
      : r.spawnError
        ? "failed"
        : "stopped";
  return `  ${String(r.id).padStart(3)}  ${state.padEnd(24)}  ${age}s ago   $ ${r.command}`;
}

function tailLines(s: string, n: number): string {
  if (!s) return "";
  const lines = s.split("\n");
  if (lines.length <= n) return s;
  const dropped = lines.length - n;
  return [`[… ${dropped} earlier lines …]`, ...lines.slice(-n)].join("\n");
}

export function formatCommandResult(cmd: string, r: RunCommandResult): string {
  const header = r.timedOut
    ? `$ ${cmd}\n[killed after timeout]`
    : `$ ${cmd}\n[exit ${r.exitCode ?? "?"}]`;
  return r.output ? `${header}\n${r.output}` : header;
}
