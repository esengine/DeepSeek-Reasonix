/** Shell-command hooks; project scope first, then global. Exit 0=pass, 2=block on Pre*, other=warn. */

import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { t } from "./i18n/index.js";

export type HookEvent = "PreToolUse" | "PostToolUse" | "UserPromptSubmit" | "Stop";

/** All four events as a const array — drives slash listing + validation. */
export const HOOK_EVENTS: readonly HookEvent[] = [
  "PreToolUse",
  "PostToolUse",
  "UserPromptSubmit",
  "Stop",
] as const;

/** Only the gating events can block the loop. */
const BLOCKING_EVENTS: ReadonlySet<HookEvent> = new Set(["PreToolUse", "UserPromptSubmit"]);

/** Per-event default timeout. Tool/prompt hooks gate progress, so they're tight. */
const DEFAULT_TIMEOUTS_MS: Record<HookEvent, number> = {
  PreToolUse: 5_000,
  UserPromptSubmit: 5_000,
  PostToolUse: 30_000,
  Stop: 30_000,
};

export type HookScope = "project" | "global";

export interface HookConfig {
  /** Anchored regex; `"*"` / omitted = every tool. Pre/PostToolUse only. */
  match?: string;
  /** Shell command to run. Spawned through the platform shell. */
  command: string;
  /** Optional human description — surfaced in `/hooks`. */
  description?: string;
  /** Per-hook timeout override in ms. */
  timeout?: number;
  /** Defaults: project scope → project root; global scope → process.cwd(). */
  cwd?: string;
}

/** Shape of `<scope>/.reasonix/settings.json` — only `hooks` for now. */
export interface HookSettings {
  hooks?: Partial<Record<HookEvent, HookConfig[]>>;
}

/** A loaded hook with its origin scope baked in (used for ordering and `/hooks`). */
export interface ResolvedHook extends HookConfig {
  event: HookEvent;
  scope: HookScope;
  /** Absolute path to the settings.json the hook came from. */
  source: string;
}

/** Outcome of a single hook invocation. */
export interface HookOutcome {
  /** Which hook fired. */
  hook: ResolvedHook;
  /** pass=exit 0; block=exit 2 on blocking event; warn=other non-zero; timeout=killed; error=spawn failed. */
  decision: "pass" | "block" | "warn" | "timeout" | "error";
  exitCode: number | null;
  /** Captured stdout (trimmed). May be empty. */
  stdout: string;
  /** Captured stderr (trimmed). The block / warn message comes from here. */
  stderr: string;
  durationMs: number;
  /** Output crossed the per-stream byte cap; surfaced so user knows we kept less than the script wrote. */
  truncated?: boolean;
}

/** Aggregate report for `runHooks`. */
export interface HookReport {
  event: HookEvent;
  outcomes: HookOutcome[];
  /** True iff at least one outcome was a `block` — only meaningful for blocking events. */
  blocked: boolean;
}

export const HOOK_SETTINGS_FILENAME = "settings.json";
export const HOOK_SETTINGS_DIRNAME = ".reasonix";

/** Where the global settings.json lives. Equivalent to `~/.reasonix/settings.json`. */
export function globalSettingsPath(homeDirOverride?: string): string {
  return join(homeDirOverride ?? homedir(), HOOK_SETTINGS_DIRNAME, HOOK_SETTINGS_FILENAME);
}

/** Where the project settings.json lives for a given root. */
export function projectSettingsPath(projectRoot: string): string {
  return join(projectRoot, HOOK_SETTINGS_DIRNAME, HOOK_SETTINGS_FILENAME);
}

function readSettingsFile(path: string): HookSettings | null {
  if (!existsSync(path)) return null;
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object") return parsed as HookSettings;
  } catch {
    /* malformed JSON → treat as no hooks; do NOT throw, the user
     * shouldn't lose the whole CLI to a typo in their settings */
  }
  return null;
}

/** Project hooks fire before global; within a scope, array order. */
export interface LoadHookSettingsOptions {
  /** Absolute project root, if any. Without it, only global hooks load. */
  projectRoot?: string;
  /** Override `~` for tests. */
  homeDir?: string;
}

export function loadHooks(opts: LoadHookSettingsOptions = {}): ResolvedHook[] {
  const out: ResolvedHook[] = [];
  if (opts.projectRoot) {
    const projPath = projectSettingsPath(opts.projectRoot);
    const settings = readSettingsFile(projPath);
    if (settings) appendResolved(out, settings, "project", projPath);
  }
  const globalPath = globalSettingsPath(opts.homeDir);
  const settings = readSettingsFile(globalPath);
  if (settings) appendResolved(out, settings, "global", globalPath);
  return out;
}

function appendResolved(
  out: ResolvedHook[],
  settings: HookSettings,
  scope: HookScope,
  source: string,
): void {
  if (!settings.hooks) return;
  for (const event of HOOK_EVENTS) {
    const list = settings.hooks[event];
    if (!Array.isArray(list)) continue;
    for (const cfg of list) {
      if (!cfg || typeof cfg.command !== "string" || cfg.command.trim() === "") continue;
      out.push({ ...cfg, event, scope, source });
    }
  }
}

/** Match field is an ANCHORED regex — `"file"` won't trigger on `read_file`; use `".*file"`. */
export function matchesTool(hook: ResolvedHook, toolName: string): boolean {
  if (hook.event !== "PreToolUse" && hook.event !== "PostToolUse") return true;
  const m = hook.match;
  if (!m || m === "*") return true;
  try {
    const re = new RegExp(`^(?:${m})$`);
    return re.test(toolName);
  } catch {
    /* malformed regex → don't fire (safer than firing on every tool) */
    return false;
  }
}

/** Payload envelope passed to hook stdin. */
export interface HookPayload {
  event: HookEvent;
  cwd: string;
  toolName?: string;
  toolArgs?: unknown;
  toolResult?: string;
  prompt?: string;
  lastAssistantText?: string;
  turn?: number;
}

/** Test seam — same shape as Node's spawn but returns a Promise of the raw outcome bits. */
export interface HookSpawnInput {
  command: string;
  cwd: string;
  stdin: string;
  timeoutMs: number;
}

export interface HookSpawnResult {
  exitCode: number | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  /** True iff spawn() itself failed (ENOENT, EACCES, …). */
  spawnError?: Error;
  /** Output capped at byte limit — hook ran to completion but consumers see clipped view. */
  truncated?: boolean;
}

/** Per-stream cap — bounds heap exposure to a runaway child between spawn and timeout. */
const HOOK_OUTPUT_CAP_BYTES = 256 * 1024;

export type HookSpawner = (input: HookSpawnInput) => Promise<HookSpawnResult>;

/** `shell: true` — hook is a shell command by contract; pipes / `&&` / env expansion must work. */
function defaultSpawner(input: HookSpawnInput): Promise<HookSpawnResult> {
  return new Promise<HookSpawnResult>((resolve) => {
    const child = spawn(input.command, {
      cwd: input.cwd,
      shell: true,
      stdio: ["pipe", "pipe", "pipe"],
    });
    // Collect raw bytes per stream and decode once at close so a
    // multi-byte UTF-8 sequence split across data chunks doesn't
    // corrupt — same approach shell.ts uses for run_command output.
    const stdoutChunks: Buffer[] = [];
    const stderrChunks: Buffer[] = [];
    let stdoutBytes = 0;
    let stderrBytes = 0;
    let truncated = false;
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGTERM");
      // SIGTERM may not land on Windows for shell children — followed
      // by a hard kill a moment later if the process is still around.
      setTimeout(() => {
        try {
          child.kill("SIGKILL");
        } catch {
          /* already gone */
        }
      }, 500);
    }, input.timeoutMs);

    const onChunk = (kind: "stdout" | "stderr", chunk: Buffer) => {
      const target = kind === "stdout" ? stdoutChunks : stderrChunks;
      const seen = kind === "stdout" ? stdoutBytes : stderrBytes;
      if (seen >= HOOK_OUTPUT_CAP_BYTES) {
        truncated = true;
        return;
      }
      const remaining = HOOK_OUTPUT_CAP_BYTES - seen;
      if (chunk.length > remaining) {
        target.push(chunk.subarray(0, remaining));
        if (kind === "stdout") stdoutBytes = HOOK_OUTPUT_CAP_BYTES;
        else stderrBytes = HOOK_OUTPUT_CAP_BYTES;
        truncated = true;
      } else {
        target.push(chunk);
        if (kind === "stdout") stdoutBytes += chunk.length;
        else stderrBytes += chunk.length;
      }
    };
    child.stdout.on("data", (chunk: Buffer) => onChunk("stdout", chunk));
    child.stderr.on("data", (chunk: Buffer) => onChunk("stderr", chunk));
    child.once("error", (err) => {
      clearTimeout(timer);
      resolve({
        exitCode: null,
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8"),
        timedOut: false,
        spawnError: err,
        truncated: truncated || undefined,
      });
    });
    child.once("close", (code) => {
      clearTimeout(timer);
      resolve({
        exitCode: code,
        stdout: Buffer.concat(stdoutChunks).toString("utf8").trim(),
        stderr: Buffer.concat(stderrChunks).toString("utf8").trim(),
        timedOut,
        truncated: truncated || undefined,
      });
    });

    try {
      child.stdin.write(input.stdin);
      child.stdin.end();
    } catch {
      /* stdin write can race with spawn errors; the close handler
       * still fires with exit 0/null */
    }
  });
}

export function formatHookOutcomeMessage(outcome: HookOutcome): string {
  if (outcome.decision === "pass") return "";
  const detail = (outcome.stderr || outcome.stdout || "").trim();
  const tag = `${outcome.hook.scope}/${outcome.hook.event}`;
  const cmd =
    outcome.hook.command.length > 60
      ? `${outcome.hook.command.slice(0, 60)}…`
      : outcome.hook.command;
  const truncTag = outcome.truncated ? t("hooks.truncated") : "";
  const decision = t(`hooks.decision${capitalize(outcome.decision)}`);
  return detail
    ? t("hooks.headWithDetail", { tag, cmd, decision, truncTag, detail })
    : t("hooks.head", { tag, cmd, decision, truncTag });
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export function decideOutcome(
  event: HookEvent,
  raw: HookSpawnResult,
): "pass" | "block" | "warn" | "timeout" | "error" {
  if (raw.spawnError) return "error";
  if (raw.timedOut) return BLOCKING_EVENTS.has(event) ? "block" : "warn";
  if (raw.exitCode === 0) return "pass";
  if (raw.exitCode === 2 && BLOCKING_EVENTS.has(event)) return "block";
  return "warn";
}

export interface RunHooksOptions {
  payload: HookPayload;
  hooks: ResolvedHook[];
  /** Test seam — defaults to a real `spawn`. */
  spawner?: HookSpawner;
}

/** Stops at first `block` so a gating hook can prevent later hooks running against a phantom success. */
export async function runHooks(opts: RunHooksOptions): Promise<HookReport> {
  const spawner = opts.spawner ?? defaultSpawner;
  const event = opts.payload.event;
  const toolName = opts.payload.toolName ?? "";
  const matching = opts.hooks.filter((h) => h.event === event && matchesTool(h, toolName));

  const outcomes: HookOutcome[] = [];
  let blocked = false;
  const stdin = `${JSON.stringify(opts.payload)}\n`;

  for (const hook of matching) {
    const start = Date.now();
    const timeoutMs = hook.timeout ?? DEFAULT_TIMEOUTS_MS[event];
    const cwd = hook.cwd ?? opts.payload.cwd;
    const raw = await spawner({ command: hook.command, cwd, stdin, timeoutMs });
    const decision = decideOutcome(event, raw);
    outcomes.push({
      hook,
      decision,
      exitCode: raw.exitCode,
      stdout: raw.stdout,
      stderr:
        raw.stderr ||
        (raw.spawnError ? raw.spawnError.message : "") ||
        (raw.timedOut ? `hook timed out after ${timeoutMs}ms` : ""),
      durationMs: Date.now() - start,
      truncated: raw.truncated,
    });
    if (decision === "block") {
      blocked = true;
      break;
    }
  }

  return { event, outcomes, blocked };
}
