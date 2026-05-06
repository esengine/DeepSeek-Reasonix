/** Background process registry for never-exiting commands; ready-signal detection short-circuits the startup wait. */

import { type ChildProcess, type SpawnOptions, spawn } from "node:child_process";
import * as pathMod from "node:path";
import { detectShellOperator, prepareSpawn, tokenizeCommand } from "./shell.js";

/** Kills the whole tree — `child.kill` only hits the direct child, leaving npm-spawned dev servers orphaned. */
function killProcessTree(pid: number, signal: "SIGTERM" | "SIGKILL"): void {
  if (process.platform === "win32") {
    // taskkill: /T = tree, /F = force (TerminateProcess, no cleanup).
    // Graceful path still uses /F on Windows because there's no signal
    // in the POSIX sense — the closest equivalent is Ctrl+Break, which
    // is unreliable from another console. /F with /T is what most
    // process managers ship on Windows.
    const args = ["/pid", String(pid), "/T"];
    if (signal === "SIGKILL") args.push("/F");
    try {
      const killer = spawn("taskkill", args, {
        stdio: "ignore",
        windowsHide: true,
      });
      // Swallow ENOENT / EACCES — we did our best. Not awaiting is
      // intentional: taskkill can take a few hundred ms and the caller
      // already has its own deadline.
      killer.on("error", () => {
        /* ignore */
      });
    } catch {
      /* ignore */
    }
    return;
  }
  // POSIX: negative pid signals the whole process group. Requires the
  // spawn to have been detached (which `start()` does below).
  try {
    process.kill(-pid, signal);
    return;
  } catch {
    /* group-kill failed — fall back to direct */
  }
  try {
    process.kill(pid, signal);
  } catch {
    /* ignore — already dead */
  }
}

/** Per-job output ring. Capped so a chatty dev server doesn't OOM. */
const DEFAULT_OUTPUT_CAP_BYTES = 64 * 1024; // 64 KB

/** First match cuts startup wait short; conservative patterns — a false negative costs a real stall. */
const READY_SIGNALS: ReadonlyArray<RegExp> = [
  // HTTP server banners
  /\blistening on\b/i,
  /\blocal:\s+https?:\/\//i,
  /\bhttps?:\/\/(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?\b/i,
  /\b(?:ready|server started|started server|app listening)\b/i,
  // Bundlers / compilers
  /\bcompiled successfully\b/i,
  /\bbuild complete(?:d)?\b/i,
  /\bwatching for (?:file )?changes\b/i,
  /\bready in \d+/i,
  // Generic
  /\bstartup (?:complete|finished)\b/i,
];

export interface JobStartOptions {
  /** Absolute path to cwd for the spawned child. */
  cwd: string;
  /** Capped at 30; ready-signal match short-circuits. Default 3. */
  waitSec?: number;
  /** Signal plumbed through from the calling tool's AbortSignal. */
  signal?: AbortSignal;
  /** Total per-job output buffer cap (bytes). Default 64 KB. */
  maxBufferBytes?: number;
}

export interface JobStartResult {
  jobId: number;
  pid: number | null;
  /** True iff the child was still running at the point we returned. */
  stillRunning: boolean;
  /** True iff a READY_SIGNALS pattern matched during the wait window. */
  readyMatched: boolean;
  /** Preview of combined stdout+stderr accumulated during the wait. */
  preview: string;
  /** If the child exited during the wait, its exit code; else null. */
  exitCode: number | null;
}

export interface JobRecord {
  id: number;
  command: string;
  pid: number | null;
  startedAt: number;
  /** Exit code once the process terminates; null while running. */
  exitCode: number | null;
  /** Combined stdout+stderr, ring-trimmed. */
  output: string;
  /** Counts all bytes the child wrote, not just what's still buffered in `output`. */
  totalBytesWritten: number;
  /** True iff the child is still alive. */
  running: boolean;
  /** Error from spawn() itself (ENOENT, etc.) once surfaced. */
  spawnError?: string;
}

export class JobRegistry {
  private readonly jobs = new Map<number, InternalJob>();
  private nextId = 1;

  /** Resolves on (a) ready signal, (b) early exit, or (c) waitSec deadline — child keeps running regardless. */
  async start(command: string, opts: JobStartOptions): Promise<JobStartResult> {
    const trimmed = command.trim();
    if (!trimmed) throw new Error("run_background: empty command");
    const op = detectShellOperator(trimmed);
    if (op !== null) {
      throw new Error(
        `run_background: shell operator "${op}" is not supported — spawn one process per background job. Compose via your orchestration, not the shell.`,
      );
    }
    const argv = tokenizeCommand(trimmed);
    if (argv.length === 0) throw new Error("run_background: empty command");
    const waitMs = Math.max(0, Math.min(30, opts.waitSec ?? 3)) * 1000;
    const maxBytes = opts.maxBufferBytes ?? DEFAULT_OUTPUT_CAP_BYTES;

    const { bin, args, spawnOverrides } = prepareSpawn(argv);
    const spawnOpts: SpawnOptions = {
      cwd: pathMod.resolve(opts.cwd),
      shell: false,
      windowsHide: true,
      env: process.env,
      // POSIX: detach so the child becomes its own process-group leader.
      // Required for `process.kill(-pid, …)` later — without it a group
      // kill fails and we end up only signaling the wrapper, leaving
      // grandchildren (node → vite → esbuild …) orphaned.
      // Windows: detached would spawn a new console window; leave the
      // default and use taskkill /T for tree termination.
      detached: process.platform !== "win32",
      ...spawnOverrides,
    };

    let child: ChildProcess;
    try {
      child = spawn(bin, args, spawnOpts);
    } catch (err) {
      // Can't even spawn — record a dead job so the model sees the
      // failure in list_jobs, and return a synthetic result.
      const id = this.nextId++;
      const job: InternalJob = {
        id,
        command: trimmed,
        pid: null,
        startedAt: Date.now(),
        exitCode: null,
        output: `[spawn failed] ${(err as Error).message}`,
        totalBytesWritten: 0,
        running: false,
        spawnError: (err as Error).message,
        child: null,
        readyPromise: Promise.resolve(),
        signalReady: () => {},
        closedPromise: Promise.resolve(),
        signalClosed: () => {},
      };
      this.jobs.set(id, job);
      return {
        jobId: id,
        pid: null,
        stillRunning: false,
        readyMatched: false,
        preview: job.output,
        exitCode: null,
      };
    }

    const id = this.nextId++;
    let readyResolve: () => void = () => {};
    const readyPromise = new Promise<void>((res) => {
      readyResolve = res;
    });
    let closedResolve: () => void = () => {};
    const closedPromise = new Promise<void>((res) => {
      closedResolve = res;
    });
    const job: InternalJob = {
      id,
      command: trimmed,
      pid: child.pid ?? null,
      startedAt: Date.now(),
      exitCode: null,
      output: "",
      totalBytesWritten: 0,
      running: true,
      child,
      readyPromise,
      signalReady: readyResolve,
      closedPromise,
      signalClosed: closedResolve,
    };
    this.jobs.set(id, job);

    let readyMatched = false;
    // Sliding window for cross-chunk ready-signal matching. A banner
    // line might land split across two reads — we want the regex to
    // see it as one piece — but testing against the full `job.output`
    // (which can be tens of KB by the time the server is up) is
    // O(N²) when 9 regexes each run on a growing buffer per chunk.
    // 1KB is comfortably bigger than any banner line we look for and
    // bounds the per-chunk regex cost regardless of total output.
    let recentForReady = "";
    const READY_WINDOW = 1024;
    const onData = (chunk: Buffer | string) => {
      const s = chunk.toString();
      job.totalBytesWritten += s.length;
      job.output += s;
      if (job.output.length > maxBytes) {
        // Drop the oldest bytes, but keep a marker so the model can see
        // output was truncated. Trim on a rough line boundary to avoid
        // chopping a line mid-sentence.
        const overflow = job.output.length - maxBytes;
        const cut = job.output.indexOf("\n", overflow);
        const start = cut >= 0 ? cut + 1 : overflow;
        job.output = `[… older output dropped …]\n${job.output.slice(start)}`;
      }
      if (!readyMatched) {
        recentForReady = (recentForReady + s).slice(-READY_WINDOW);
        for (const re of READY_SIGNALS) {
          if (re.test(recentForReady)) {
            readyMatched = true;
            job.signalReady();
            break;
          }
        }
      }
    };
    child.stdout?.on("data", onData);
    child.stderr?.on("data", onData);
    child.on("error", (err) => {
      job.running = false;
      job.spawnError = err.message;
      job.signalReady();
      job.signalClosed();
    });
    child.on("close", (code) => {
      job.running = false;
      job.exitCode = code;
      job.signalReady();
      job.signalClosed();
    });

    const onAbort = () => this.stop(id, { graceMs: 100 });
    if (opts.signal?.aborted) {
      onAbort();
    } else {
      opts.signal?.addEventListener("abort", onAbort, { once: true });
    }

    // Race: (a) ready signal, (b) child exit, (c) wait deadline.
    let timer: ReturnType<typeof setTimeout> | null = null;
    await Promise.race([
      readyPromise,
      new Promise<void>((res) => {
        timer = setTimeout(res, waitMs);
      }),
    ]);
    if (timer) clearTimeout(timer);

    return {
      jobId: id,
      pid: job.pid,
      stillRunning: job.running,
      readyMatched,
      preview: job.output,
      exitCode: job.exitCode,
    };
  }

  read(id: number, opts: { since?: number; tailLines?: number } = {}): JobReadResult | null {
    const job = this.jobs.get(id);
    if (!job) return null;
    const full = job.output;
    let slice = full;
    if (typeof opts.since === "number" && opts.since >= 0 && opts.since < full.length) {
      slice = full.slice(opts.since);
    }
    if (typeof opts.tailLines === "number" && opts.tailLines > 0) {
      const lines = slice.split("\n");
      const keep = lines.slice(Math.max(0, lines.length - opts.tailLines));
      slice = keep.join("\n");
    }
    return {
      output: slice,
      byteLength: full.length,
      running: job.running,
      exitCode: job.exitCode,
      command: job.command,
      pid: job.pid,
      spawnError: job.spawnError,
    };
  }

  /** SIGTERM, wait graceMs, then SIGKILL. Idempotent on already-exited jobs. */
  async stop(id: number, opts: { graceMs?: number } = {}): Promise<JobRecord | null> {
    const job = this.jobs.get(id);
    if (!job) return null;
    if (!job.running || !job.child) return snapshot(job);
    const graceMs = Math.max(0, opts.graceMs ?? 2000);
    // Tree kill — reaches grandchildren (vite, esbuild, etc.) instead
    // of just the npm/cmd.exe wrapper that our direct child represents.
    // Falls back to child.kill() only when we somehow don't have a pid.
    if (job.pid !== null) {
      killProcessTree(job.pid, "SIGTERM");
    } else {
      try {
        job.child.kill("SIGTERM");
      } catch {
        /* already dead — fall through */
      }
    }
    // closedPromise (not readyPromise) — readyPromise can have fired at
    // startup on a ready-signal regex match, which would short-circuit
    // this race even though the process is still alive.
    await Promise.race([job.closedPromise, new Promise<void>((res) => setTimeout(res, graceMs))]);
    if (job.running) {
      if (job.pid !== null) {
        killProcessTree(job.pid, "SIGKILL");
      } else {
        try {
          job.child.kill("SIGKILL");
        } catch {
          /* ignore */
        }
      }
      // Wait for the actual close handler — a fixed timer can return
      // before Node's `close` event fires under load (Windows taskkill
      // /T /F on a three-level tree can take ~1s to propagate).
      await Promise.race([job.closedPromise, new Promise<void>((res) => setTimeout(res, 5000))]);
    }
    return snapshot(job);
  }

  list(): JobRecord[] {
    return [...this.jobs.values()].map(snapshot);
  }

  async shutdown(deadlineMs = 5000): Promise<void> {
    const start = Date.now();
    const runningJobs = [...this.jobs.values()].filter((j) => j.running && j.child);
    if (runningJobs.length === 0) return;

    for (const job of runningJobs) {
      if (job.pid !== null) killProcessTree(job.pid, "SIGTERM");
      else
        try {
          job.child?.kill("SIGTERM");
        } catch {
          /* ignore */
        }
    }
    const allClose = Promise.all(runningJobs.map((j) => j.readyPromise));
    const elapsed = () => Date.now() - start;
    // Grace window: give well-behaved apps time to clean up, capped at
    // half the deadline so we always leave room for a SIGKILL pass +
    // reap confirmation.
    const graceMs = Math.min(1500, Math.max(0, deadlineMs / 2));
    await Promise.race([allClose, new Promise<void>((res) => setTimeout(res, graceMs))]);
    // Force-kill everything still alive.
    for (const job of runningJobs) {
      if (!job.running) continue;
      if (job.pid !== null) killProcessTree(job.pid, "SIGKILL");
      else
        try {
          job.child?.kill("SIGKILL");
        } catch {
          /* ignore */
        }
    }
    // Wait for close events post-SIGKILL. taskkill /T on Windows is
    // async — without this final wait, shutdown() can return while
    // grandchildren are still mid-teardown, which is what "runningCount
    // non-zero after shutdown" looks like.
    const remaining = Math.max(800, deadlineMs - elapsed());
    await Promise.race([allClose, new Promise<void>((res) => setTimeout(res, remaining))]);
  }

  /** Count of still-running jobs — drives the TUI status-bar indicator. */
  runningCount(): number {
    let n = 0;
    for (const job of this.jobs.values()) if (job.running) n++;
    return n;
  }
}

interface InternalJob extends JobRecord {
  /** Underlying Node child process. Null only on spawn failure. */
  child: ChildProcess | null;
  /** Resolved when ready-signal fires OR the child exits. */
  readyPromise: Promise<void>;
  /** Fires readyPromise — called by ready-signal OR close/error handlers. */
  signalReady: () => void;
  /** Resolves only on close/error — never on ready-signal. Used by stop() to wait for actual exit. */
  closedPromise: Promise<void>;
  signalClosed: () => void;
}

export interface JobReadResult {
  output: string;
  /** Total bytes ever in the buffer (pre-slice). Caller passes back as `since`. */
  byteLength: number;
  running: boolean;
  exitCode: number | null;
  command: string;
  pid: number | null;
  spawnError?: string;
}

function snapshot(job: InternalJob): JobRecord {
  return {
    id: job.id,
    command: job.command,
    pid: job.pid,
    startedAt: job.startedAt,
    exitCode: job.exitCode,
    output: job.output,
    totalBytesWritten: job.totalBytesWritten,
    running: job.running,
    spawnError: job.spawnError,
  };
}
