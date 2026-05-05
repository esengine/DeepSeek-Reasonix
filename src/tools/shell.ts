/** cwd pinned to root; non-allowlisted commands throw to a UI confirm gate; spawn is `shell: false`, tokenized argv only. */

import { type ChildProcess, type SpawnOptions, spawn, spawnSync } from "node:child_process";
import { existsSync, statSync } from "node:fs";
import * as pathMod from "node:path";
import type { ToolRegistry } from "../tools.js";
import { JobRegistry } from "./jobs.js";
import {
  type CommandChain,
  UnsupportedSyntaxError,
  chainAllowed,
  parseCommandChain,
  runChain,
} from "./shell-chain.js";

/** Kill child + descendants. Windows: taskkill /T /F. Unix: SIGKILL the process group when detached, else fall back to SIGKILL on the leader. */
export function killProcessTree(child: ChildProcess): void {
  if (!child.pid || child.killed) return;
  if (process.platform === "win32") {
    try {
      spawnSync("taskkill", ["/pid", String(child.pid), "/T", "/F"], {
        stdio: "ignore",
        windowsHide: true,
      });
      return;
    } catch {
      /* fall through to SIGKILL */
    }
  }
  try {
    process.kill(-child.pid, "SIGKILL");
    return;
  } catch {
    /* not a process group leader — fall through */
  }
  try {
    child.kill("SIGKILL");
  } catch {
    /* already gone */
  }
}

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

const DEFAULT_TIMEOUT_SEC = 60;
const DEFAULT_MAX_OUTPUT_CHARS = 32_000;

/** Read-only reports + test runners whose failure mode is "exit 1 with output". */
export const BUILTIN_ALLOWLIST: ReadonlyArray<string> = [
  // Repo inspection
  "git status",
  "git diff",
  "git log",
  "git show",
  "git blame",
  "git branch",
  "git remote",
  "git rev-parse",
  "git config --get",
  // Filesystem inspection
  "ls",
  "pwd",
  "cat",
  "head",
  "tail",
  "wc",
  "file",
  "tree",
  "find",
  "grep",
  "rg",
  // Language version probes
  "node --version",
  "node -v",
  "npm --version",
  "npx --version",
  "python --version",
  "python3 --version",
  "cargo --version",
  "go version",
  "rustc --version",
  "deno --version",
  "bun --version",
  // Test runners (non-destructive by convention)
  "npm test",
  "npm run test",
  "npx vitest run",
  "npx vitest",
  "npx jest",
  "pytest",
  "python -m pytest",
  "cargo test",
  "cargo check",
  "cargo clippy",
  "go test",
  "go vet",
  "deno test",
  "bun test",
  // Linters / typecheckers (read-only by convention)
  "npm run lint",
  "npm run typecheck",
  "npx tsc --noEmit",
  "npx biome check",
  "npx eslint",
  "npx prettier --check",
  "ruff",
  "mypy",
];

/** No env / glob / backtick / `$(…)` expansion — prevents bypass of allowlist via concatenation. */
export function tokenizeCommand(cmd: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i]!;
    if (quote) {
      if (ch === quote) {
        quote = null;
      } else if (ch === "\\" && quote === '"' && i + 1 < cmd.length) {
        cur += cmd[++i];
      } else {
        cur += ch;
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === " " || ch === "\t") {
      if (cur.length > 0) {
        out.push(cur);
        cur = "";
      }
      continue;
    }
    cur += ch;
  }
  if (quote) throw new Error(`unclosed ${quote} in command`);
  if (cur.length > 0) out.push(cur);
  return out;
}

/** Up-front detection — without it, `dir | findstr foo` quotes `|` literal and pipe silently fails. */
export function detectShellOperator(cmd: string): string | null {
  const opPrefix = /^(?:2>&1|&>|\|{1,2}|&{1,2}|2>{1,2}|>{1,2}|<{1,2})/;
  let cur = "";
  let curQuoted = false;
  let quote: '"' | "'" | null = null;
  const check = (): string | null => {
    if (cur.length === 0 && !curQuoted) return null;
    if (!curQuoted) {
      const m = opPrefix.exec(cur);
      if (m) return m[0] ?? null;
    }
    return null;
  };
  for (let i = 0; i < cmd.length; i++) {
    const ch = cmd[i]!;
    if (quote) {
      if (ch === quote) {
        quote = null;
      } else if (ch === "\\" && quote === '"' && i + 1 < cmd.length) {
        cur += cmd[++i];
        curQuoted = true;
      } else {
        cur += ch;
        curQuoted = true;
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      curQuoted = true;
      continue;
    }
    if (ch === " " || ch === "\t") {
      const op = check();
      if (op) return op;
      cur = "";
      curQuoted = false;
      continue;
    }
    cur += ch;
  }
  if (quote) return null; // let tokenizeCommand throw the unclosed-quote error
  return check();
}

/** Match on space-normalized leading tokens — `git   status  -s` matches the `git status` prefix. */
export function isAllowed(cmd: string, extra: readonly string[] = []): boolean {
  const normalized = cmd.trim().replace(/\s+/g, " ");
  const allowlist = [...BUILTIN_ALLOWLIST, ...extra];
  for (const prefix of allowlist) {
    if (normalized === prefix) return true;
    if (normalized.startsWith(`${prefix} `)) return true;
  }
  return false;
}

/** For chain commands, every segment must individually clear the allowlist. */
export function isCommandAllowed(cmd: string, extra: readonly string[] = []): boolean {
  let chain: CommandChain | null;
  try {
    chain = parseCommandChain(cmd);
  } catch {
    return false;
  }
  if (chain === null) return isAllowed(cmd, extra);
  return chainAllowed(chain, (seg) => isAllowed(seg, extra));
}

export interface RunCommandResult {
  exitCode: number | null;
  /** Combined stdout+stderr, truncated to `maxOutputChars` with a marker. */
  output: string;
  /** True when the process was killed for exceeding `timeoutSec`. */
  timedOut: boolean;
}

export async function runCommand(
  cmd: string,
  opts: {
    cwd: string;
    timeoutSec?: number;
    maxOutputChars?: number;
    signal?: AbortSignal;
  },
): Promise<RunCommandResult> {
  const timeoutSec = opts.timeoutSec ?? DEFAULT_TIMEOUT_SEC;
  const maxChars = opts.maxOutputChars ?? DEFAULT_MAX_OUTPUT_CHARS;
  const argv = tokenizeCommand(cmd);
  if (argv.length === 0) throw new Error("run_command: empty command");
  const chain = parseCommandChain(cmd);
  if (chain !== null) {
    return await runChain(chain, {
      cwd: opts.cwd,
      timeoutSec,
      maxOutputChars: maxChars,
      signal: opts.signal,
    });
  }
  const timeoutMs = timeoutSec * 1000;

  const spawnOpts: SpawnOptions = {
    cwd: opts.cwd,
    shell: false, // no shell-expansion — see header comment
    windowsHide: true,
    // PYTHONIOENCODING + PYTHONUTF8 force any spawned Python child
    // (run_command running `python script.py`, etc.) to emit UTF-8
    // on stdout/stderr. Without this, Chinese-Windows defaults
    // Python's stdout encoder to GBK and `print("…")` raises
    // UnicodeEncodeError on emoji / non-GBK chars — the model then
    // sees a Python traceback instead of the script's real output
    // and goes around in circles trying to fix the wrong problem.
    // Harmless on non-Python processes (env vars they don't read).
    env: { ...process.env, PYTHONIOENCODING: "utf-8", PYTHONUTF8: "1" },
  };

  // Windows: two layered fixes on top of shell:false —
  //   1. Resolve bare command names via PATH × PATHEXT (CreateProcess
  //      ignores PATHEXT, so `npm` alone misses `npm.cmd`).
  //   2. Node 21.7.3+ (CVE-2024-27980) refuses to spawn `.cmd`/`.bat`
  //      directly even with shell:false and safe args — throws
  //      EINVAL at invocation time. Wrap those via `cmd.exe /d /s /c`
  //      with verbatim args + manual quoting, so shell metacharacters
  //      in arguments stay literal.
  // Unix path is unchanged.
  const { bin, args, spawnOverrides } = prepareSpawn(argv);
  const effectiveSpawnOpts = { ...spawnOpts, ...spawnOverrides };

  return await new Promise<RunCommandResult>((resolve, reject) => {
    let child: import("node:child_process").ChildProcess;
    try {
      child = spawn(bin, args, effectiveSpawnOpts);
    } catch (err) {
      reject(err);
      return;
    }
    // Collect raw Buffer chunks rather than decoding incrementally —
    // a multi-byte sequence can land split across chunks, and a naïve
    // chunk.toString() corrupts it before the second half arrives.
    // We decode once at close time, where smartDecodeOutput can also
    // sniff non-UTF-8 codepages cleanly. The byte cap mirrors the
    // prior char cap (2× maxChars worth) so a chatty process can't
    // OOM us.
    const chunks: Buffer[] = [];
    let totalBytes = 0;
    const byteCap = maxChars * 2 * 4; // worst-case 4 bytes/char for utf-8/gbk
    let timedOut = false;
    let aborted = false;
    const killChildTree = () => killProcessTree(child);
    const killTimer = setTimeout(() => {
      timedOut = true;
      killChildTree();
    }, timeoutMs);
    const onAbort = () => {
      aborted = true;
      killChildTree();
    };
    // Check synchronously first — if the signal aborted before listener attach
    // (parent loop was already cancelled), addEventListener with `once:true`
    // never fires, child runs unbounded.
    if (opts.signal?.aborted) {
      onAbort();
    } else {
      opts.signal?.addEventListener("abort", onAbort, { once: true });
    }

    const onData = (chunk: Buffer | string) => {
      const b = typeof chunk === "string" ? Buffer.from(chunk) : chunk;
      if (totalBytes >= byteCap) return;
      const remaining = byteCap - totalBytes;
      if (b.length > remaining) {
        chunks.push(b.subarray(0, remaining));
        totalBytes = byteCap;
      } else {
        chunks.push(b);
        totalBytes += b.length;
      }
    };
    child.stdout?.on("data", onData);
    child.stderr?.on("data", onData);
    child.on("error", (err) => {
      clearTimeout(killTimer);
      opts.signal?.removeEventListener("abort", onAbort);
      reject(err);
    });
    child.on("close", (code) => {
      clearTimeout(killTimer);
      opts.signal?.removeEventListener("abort", onAbort);
      const merged = Buffer.concat(chunks);
      const buf = smartDecodeOutput(merged);
      const output =
        buf.length > maxChars
          ? `${buf.slice(0, maxChars)}\n\n[… truncated ${buf.length - maxChars} chars …]`
          : buf;
      resolve({ exitCode: code, output, timedOut });
    });
  });
}

/** GBK fallback on Windows — cmd.exe's localized error DLL and native EXE stderr ignore chcp 65001. */
export function smartDecodeOutput(buf: Buffer): string {
  if (buf.length === 0) return "";
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(buf);
  } catch {
    // Fall through to platform-specific fallback.
  }
  if (process.platform === "win32") {
    try {
      // TextDecoder supports gbk / gb18030 in Node 18+ via the WHATWG
      // Encoding spec. gb18030 is the modern superset; falling back
      // to it covers GBK byte sequences plus the rare 4-byte CJK
      // characters that appear in newer system messages.
      return new TextDecoder("gb18030").decode(buf);
    } catch {
      // Decoder unavailable in this build — fall through.
    }
  }
  // Last resort: lossy UTF-8 with replacement chars. The model still
  // gets "something happened" with the structural exit-code marker
  // intact, which is more useful than throwing away the entire output.
  return buf.toString("utf8");
}

export interface ResolveExecutableOptions {
  platform?: NodeJS.Platform;
  env?: { PATH?: string; PATHEXT?: string };
  isFile?: (path: string) => boolean;
  pathDelimiter?: string;
}

/** CreateProcess ignores PATHEXT — bare `npm` fails ENOENT under `shell:false` without this resolver. */
export function resolveExecutable(cmd: string, opts: ResolveExecutableOptions = {}): string {
  const platform = opts.platform ?? process.platform;
  if (platform !== "win32") return cmd;
  if (!cmd) return cmd;
  // Already a path fragment — spawn handles these natively.
  if (cmd.includes("/") || cmd.includes("\\") || pathMod.isAbsolute(cmd)) return cmd;
  // If the model wrote `npm.cmd` explicitly, respect that verbatim.
  if (pathMod.extname(cmd)) return cmd;

  const env = opts.env ?? process.env;
  const pathExt = (env.PATHEXT ?? ".COM;.EXE;.BAT;.CMD")
    .split(";")
    .map((e) => e.trim())
    .filter(Boolean);
  const delimiter = opts.pathDelimiter ?? (platform === "win32" ? ";" : pathMod.delimiter);
  const pathDirs = (env.PATH ?? "").split(delimiter).filter(Boolean);
  const isFile = opts.isFile ?? defaultIsFile;

  for (const dir of pathDirs) {
    for (const ext of pathExt) {
      // Force win32 join so CI tests that pass `platform: "win32"`
      // from a Linux runner get backslash-joined paths; the real-
      // Windows runtime path lands here too and gets the correct
      // separator regardless of where pathMod defaults.
      const full = pathMod.win32.join(dir, cmd + ext);
      if (isFile(full)) return full;
    }
  }
  return cmd;
}

function defaultIsFile(full: string): boolean {
  try {
    return existsSync(full) && statSync(full).isFile();
  } catch {
    return false;
  }
}

/** Windows workarounds: PATHEXT lookup + CVE-2024-27980 prohibition on direct `.cmd`/`.bat` spawn. */
export function prepareSpawn(
  argv: readonly string[],
  opts: ResolveExecutableOptions = {},
): { bin: string; args: string[]; spawnOverrides: SpawnOptions } {
  const head = argv[0] ?? "";
  const tail = argv.slice(1);
  const platform = opts.platform ?? process.platform;
  const resolved = resolveExecutable(head, opts);

  if (platform !== "win32") {
    return { bin: resolved, args: [...tail], spawnOverrides: {} };
  }

  // `.cmd` / `.bat` wrappers require cmd.exe on post-CVE Node.
  if (/\.(cmd|bat)$/i.test(resolved)) {
    const cmdline = [resolved, ...tail].map(quoteForCmdExe).join(" ");
    return {
      bin: "cmd.exe",
      args: ["/d", "/s", "/c", withUtf8Codepage(cmdline)],
      // windowsVerbatimArguments prevents Node from re-quoting the /c
      // payload — we've already composed an exact cmd.exe command
      // line. Without this Node wraps our already-quoted string in
      // another round of quotes and cmd.exe can't parse it.
      spawnOverrides: { windowsVerbatimArguments: true },
    };
  }

  // Bare command names that PATH × PATHEXT couldn't resolve to an
  // on-disk file — these are almost always cmd.exe built-ins (`dir`,
  // `echo`, `type`, `ver`, `vol`, `where`, `help`, …) which don't
  // exist as standalone executables. Direct spawn crashes with ENOENT;
  // routing through cmd.exe lets the built-in resolve, and if it's
  // genuinely unknown the user gets the standard "'foo' is not
  // recognized" message instead of a raw spawn failure.
  if (isBareWindowsName(resolved) && resolved === head) {
    const cmdline = [head, ...tail].map(quoteForCmdExe).join(" ");
    return {
      bin: "cmd.exe",
      args: ["/d", "/s", "/c", withUtf8Codepage(cmdline)],
      spawnOverrides: { windowsVerbatimArguments: true },
    };
  }

  // PowerShell variants: chcp 65001 doesn't help here because PowerShell
  // sets its own [Console]::OutputEncoding at startup — usually system
  // codepage (CP936/CP932/CP949 on CJK Windows) or UTF-16. The result
  // is mojibake when our `chunk.toString()` UTF-8-decodes its stdout.
  // Inject a UTF-8 setup prelude into the `-Command` (or `-c`) arg so
  // any output produced thereafter is UTF-8.
  if (isPowerShellExe(resolved)) {
    const patched = injectPowerShellUtf8(tail);
    if (patched) {
      return { bin: resolved, args: patched, spawnOverrides: {} };
    }
  }

  return { bin: resolved, args: [...tail], spawnOverrides: {} };
}

/** Resolved bin path looks like Windows PowerShell or PowerShell Core. */
function isPowerShellExe(resolved: string): boolean {
  return /(?:^|[\\/])(?:powershell|pwsh)(?:\.exe)?$/i.test(resolved);
}

/** Targets `-Command` only — PowerShell quoting is finicky enough that wrapping script-file mode could break it. */
export function injectPowerShellUtf8(args: readonly string[]): string[] | null {
  const prelude =
    "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;$OutputEncoding=[System.Text.Encoding]::UTF8;";
  for (let i = 0; i < args.length; i++) {
    const a = args[i] ?? "";
    if (/^-(?:Command|c)$/i.test(a) && i + 1 < args.length) {
      const out = [...args];
      out[i + 1] = `${prelude}${args[i + 1] ?? ""}`;
      return out;
    }
  }
  return null;
}

/** Single `&` (not `&&`) so the command still runs on Win7 where chcp can return non-zero. */
export function withUtf8Codepage(cmdline: string): string {
  return `chcp 65001 >nul & ${cmdline}`;
}

function isBareWindowsName(s: string): boolean {
  if (!s) return false;
  if (s.includes("/") || s.includes("\\")) return false;
  if (pathMod.isAbsolute(s)) return false;
  if (pathMod.extname(s)) return false;
  return true;
}

/** Doubles embedded quotes per cmd.exe's `""` escape rule; bare alnum passes through unquoted. */
export function quoteForCmdExe(arg: string): string {
  if (arg === "") return '""';
  if (!/[\s"&|<>^%(),;!]/.test(arg)) return arg;
  return `"${arg.replace(/"/g, '""')}"`;
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
      "Run a shell command in the project root and return its combined stdout+stderr.\n\nConstraints (read these before the first call):\n• Chain operators `|`, `||`, `&&`, `;` ARE supported — parsed natively, no shell invoked, so semantics are identical on Windows / macOS / Linux. Each chain segment is allowlist-checked individually: `git status | grep main` runs if both halves are allowed.\n• File redirects ARE supported: `>` truncate, `>>` append, `<` stdin from file, `2>` / `2>>` stderr to file, `2>&1` merge stderr→stdout, `&>` both to file. Targets resolve relative to the project root. At most one redirect per fd per segment.\n• Background `&`, heredoc `<<`, command substitution `$(…)`, subshells `(…)`, and process substitution `<(…)` are NOT supported. Wrap a literal `&` arg in quotes; for input use a `<` file or the binary's own --input flag.\n• Env-var expansion `$VAR` is NOT performed — `$VAR` is passed as a literal string. Use the binary's own --env flag or substitute the value yourself.\n• `cd` DOES NOT PERSIST between calls — each call spawns a fresh process rooted at the project. If a tool needs a subdirectory, pass it via the tool's own flag (`npm --prefix`, `cargo -C`, `git -C`, `pytest tests/…`), NOT via a preceding `cd`.\n• Glob patterns (`*.ts`) are passed through as literal arguments — no shell expansion. Use `grep -r`, `rg`, `find -name`, etc.\n• Avoid commands with unbounded output (`netstat -ano`, `find /`, etc.) — they waste tokens. Filter at source: `netstat -ano -p TCP`, `find src -name '*.ts'`, `grep -c`, `wc -l`.\n\nCommon read-only inspection and test/lint/typecheck commands run immediately; anything that could mutate state, install dependencies, or touch the network is refused until the user confirms it in the TUI. Prefer this over asking the user to run a command manually — after edits, run the project's tests to verify.",
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
        throw new NeedsConfirmationError(cmd);
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
      if (!isAllowAll() && !isAllowed(cmd, getExtraAllowed())) {
        throw new NeedsConfirmationError(cmd);
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
