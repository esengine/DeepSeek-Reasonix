// Worker-isolated regex evaluator. V8's regex engine is uninterruptible and
// exponential for catastrophic patterns (`(a+)+!`); running re.test inside a
// Worker lets the main thread `terminate()` it on a hard deadline. The
// worker uses CommonJS because `new Worker(src, { eval: true })` doesn't
// support ESM input.
import { Worker } from "node:worker_threads";

const WORKER_SOURCE = `
const { parentPort } = require("node:worker_threads");
parentPort.on("message", (msg) => {
  const { id, text, source, flags } = msg;
  let re;
  try {
    re = new RegExp(source, flags);
  } catch (err) {
    parentPort.postMessage({ id, error: (err && err.message) ? err.message : String(err) });
    return;
  }
  const lines = text.split(/\\r?\\n/);
  const hits = [];
  for (let i = 0; i < lines.length; i++) {
    if (re.test(lines[i])) hits.push(i);
  }
  parentPort.postMessage({ id, hits });
});
`;

const DEFAULT_TIMEOUT_MS = 5_000;

type Pending = {
  resolve: (hits: number[]) => void;
  reject: (err: Error) => void;
  timer: NodeJS.Timeout;
  signal?: AbortSignal;
  onAbort?: () => void;
};

export interface RegexRunnerOptions {
  defaultTimeoutMs?: number;
}

export class RegexRunner {
  private worker: Worker | null = null;
  private readonly pending = new Map<number, Pending>();
  private nextId = 1;
  private readonly defaultTimeoutMs: number;

  constructor(opts: RegexRunnerOptions = {}) {
    this.defaultTimeoutMs = opts.defaultTimeoutMs ?? DEFAULT_TIMEOUT_MS;
  }

  testLines(
    text: string,
    source: string,
    flags: string,
    opts: { signal?: AbortSignal; timeoutMs?: number } = {},
  ): Promise<number[]> {
    return new Promise<number[]>((resolve, reject) => {
      if (opts.signal?.aborted) {
        reject(new Error("regex evaluation aborted"));
        return;
      }
      if (!this.worker) this.worker = this.spawn();
      const id = this.nextId++;
      const timeoutMs = opts.timeoutMs ?? this.defaultTimeoutMs;
      const timer = setTimeout(() => {
        this.pending.delete(id);
        // Hot regex stuck inside V8 — only way out is to kill the worker.
        // The next call respawns; one cold start (~30ms) costs less than
        // however long the runaway pattern would have taken.
        this.killWorker();
        reject(new Error(`regex evaluation exceeded ${timeoutMs}ms`));
      }, timeoutMs);
      const entry: Pending = { resolve, reject, timer };
      if (opts.signal) {
        entry.signal = opts.signal;
        entry.onAbort = () => {
          this.pending.delete(id);
          clearTimeout(timer);
          this.killWorker();
          reject(new Error("regex evaluation aborted"));
        };
        opts.signal.addEventListener("abort", entry.onAbort, { once: true });
      }
      this.pending.set(id, entry);
      this.worker.postMessage({ id, text, source, flags });
    });
  }

  async shutdown(): Promise<void> {
    if (this.worker) {
      const w = this.worker;
      this.worker = null;
      await w.terminate();
    }
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer);
      if (entry.onAbort && entry.signal) {
        entry.signal.removeEventListener("abort", entry.onAbort);
      }
      entry.reject(new Error("regex runner shut down"));
    }
    this.pending.clear();
  }

  private spawn(): Worker {
    const w = new Worker(WORKER_SOURCE, { eval: true });
    w.on("message", (msg: { id: number; hits?: number[]; error?: string }) => {
      const entry = this.pending.get(msg.id);
      if (!entry) return;
      clearTimeout(entry.timer);
      if (entry.onAbort && entry.signal) {
        entry.signal.removeEventListener("abort", entry.onAbort);
      }
      this.pending.delete(msg.id);
      if (msg.error !== undefined) entry.reject(new Error(msg.error));
      else entry.resolve(msg.hits ?? []);
    });
    w.on("error", (err) => {
      if (this.worker !== w) return;
      this.failPending(err);
    });
    w.on("exit", () => {
      // After a deliberate terminate() we've already swapped to a new
      // worker; ignore the old worker's tail event so we don't reject
      // calls that belong to its replacement.
      if (this.worker !== w) return;
      this.worker = null;
      if (this.pending.size > 0) this.failPending(new Error("regex worker exited"));
    });
    return w;
  }

  private killWorker(): void {
    if (!this.worker) return;
    const w = this.worker;
    this.worker = null;
    void w.terminate();
  }

  private failPending(err: Error): void {
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer);
      if (entry.onAbort && entry.signal) {
        entry.signal.removeEventListener("abort", entry.onAbort);
      }
      entry.reject(err);
    }
    this.pending.clear();
  }
}

let _runner: RegexRunner | null = null;

/** Process-singleton — amortises ~30 ms worker cold start across a session. */
export function getRegexRunner(): RegexRunner {
  if (!_runner) _runner = new RegexRunner();
  return _runner;
}

/** Test-only: install a runner with custom options (e.g. shorter timeout). */
export function __setRegexRunnerForTesting(runner: RegexRunner | null): void {
  void _runner?.shutdown().catch(() => undefined);
  _runner = runner;
}
