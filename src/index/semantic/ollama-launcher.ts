/** Daemon spawn is detached + unref'd so it outlives the CLI; non-TTY shells error instead of prompting. */

import { spawn, spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { probeOllama } from "./embedding.js";

export interface OllamaStatus {
  /** `ollama` binary resolvable on PATH or at the Windows installer path. */
  binaryFound: boolean;
  /** HTTP daemon reachable at the configured base URL. */
  daemonRunning: boolean;
  /** True if `<model>` (or `<model>:latest`) appears in `ollama list`. */
  modelPulled: boolean;
  /** Model the caller asked about — echoed for log clarity. */
  modelName: string;
  /** Models the daemon reported, for diagnostics. Empty when daemon down. */
  installedModels: string[];
}

/** Falls back to the Windows installer path because PATH refresh is per-shell — daemon may be up while the dashboard process inherited a stale PATH. */
export function findOllamaBinary(): string | null {
  const cmd = process.platform === "win32" ? "where" : "which";
  const out = spawnSync(cmd, ["ollama"], { encoding: "utf8" });
  if (out.status === 0) {
    const first = out.stdout.split(/\r?\n/).find((l) => l.trim().length > 0);
    if (first) return first.trim();
  }
  if (process.platform === "win32") {
    const local = process.env.LOCALAPPDATA;
    if (local) {
      const candidate = join(local, "Programs", "Ollama", "ollama.exe");
      if (existsSync(candidate)) return candidate;
    }
  }
  return null;
}

/** Treats `<model>` and `<model>:latest` as the same — Ollama appends `:latest` to plain pulls. */
export async function checkOllamaStatus(
  modelName: string,
  baseUrl?: string,
): Promise<OllamaStatus> {
  const binary = findOllamaBinary();
  const probe = await probeOllama({ baseUrl });
  const installedModels = probe.ok ? probe.models : [];
  const wanted = modelName.includes(":") ? modelName : `${modelName}:latest`;
  const modelPulled = installedModels.some((m) => m === modelName || m === wanted);
  return {
    binaryFound: binary !== null,
    daemonRunning: probe.ok,
    modelPulled,
    modelName,
    installedModels,
  };
}

/** Detached + unref'd so daemon survives the CLI; output discarded so no ghost cmd window on Windows. */
export async function startOllamaDaemon(
  opts: { baseUrl?: string; timeoutMs?: number; signal?: AbortSignal } = {},
): Promise<{ ready: boolean; pid: number | null }> {
  const timeoutMs = opts.timeoutMs ?? 15_000;
  const child = spawn(findOllamaBinary() ?? "ollama", ["serve"], {
    detached: true,
    stdio: "ignore",
    windowsHide: true,
  });
  child.unref();

  const pid = child.pid ?? null;
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (opts.signal?.aborted) return { ready: false, pid };
    const probe = await probeOllama({ baseUrl: opts.baseUrl, signal: opts.signal });
    if (probe.ok) return { ready: true, pid };
    await sleep(500);
  }
  return { ready: false, pid };
}

/** `onLine` called per line so the CLI can render its own bar instead of ollama's TTY output. */
export async function pullOllamaModel(
  modelName: string,
  opts: { onLine?: (line: string, stream: "stdout" | "stderr") => void; signal?: AbortSignal } = {},
): Promise<number> {
  return new Promise<number>((resolve) => {
    const child = spawn(findOllamaBinary() ?? "ollama", ["pull", modelName], {
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });
    if (opts.signal) {
      const onAbort = () => child.kill();
      opts.signal.addEventListener("abort", onAbort, { once: true });
      child.once("exit", () => opts.signal?.removeEventListener("abort", onAbort));
    }
    streamLines(child.stdout, (l) => opts.onLine?.(l, "stdout"));
    streamLines(child.stderr, (l) => opts.onLine?.(l, "stderr"));
    child.once("exit", (code) => resolve(code ?? -1));
    child.once("error", () => resolve(-1));
  });
}

function streamLines(stream: NodeJS.ReadableStream | null, cb: (line: string) => void): void {
  if (!stream) return;
  let buf = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk: string) => {
    buf += chunk;
    let nl = buf.indexOf("\n");
    while (nl !== -1) {
      const line = buf.slice(0, nl).replace(/\r$/, "");
      buf = buf.slice(nl + 1);
      if (line.length > 0) cb(line);
      nl = buf.indexOf("\n");
    }
  });
  stream.on("end", () => {
    if (buf.length > 0) cb(buf.replace(/\r$/, ""));
  });
}
