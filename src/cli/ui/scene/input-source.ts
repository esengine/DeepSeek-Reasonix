import { spawn } from "node:child_process";
import { DEFAULT_COMMAND } from "./renderer-process.js";

export type InputModifier = "ctrl" | "alt" | "shift" | "super";

export type KeyInputEvent = {
  event: "key";
  code: string;
  char?: string;
  modifiers?: readonly InputModifier[];
};

export type InputSource = {
  onKey(handler: (event: KeyInputEvent) => void): () => void;
  /** Send SIGINT to the child if it's still alive, then await exit. */
  close(): Promise<number | null>;
  /** Await the child's natural exit without signaling. */
  wait(): Promise<number | null>;
};

export type SpawnInputSourceOptions = {
  command?: readonly string[];
  cwd?: string;
  env?: NodeJS.ProcessEnv;
};

export const DEFAULT_INPUT_COMMAND: readonly string[] = [...DEFAULT_COMMAND, "--", "--emit-input"];

export function spawnInputSource(opts: SpawnInputSourceOptions = {}): InputSource {
  const command = opts.command ?? DEFAULT_INPUT_COMMAND;
  const [cmd, ...args] = command;
  if (!cmd) {
    throw new Error("spawnInputSource: empty command");
  }

  const child = spawn(cmd, args, {
    cwd: opts.cwd,
    env: opts.env ?? process.env,
    stdio: ["ignore", "pipe", "inherit"],
  });

  const handlers = new Set<(event: KeyInputEvent) => void>();
  let buf = "";

  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => {
    buf += chunk;
    let nl = buf.indexOf("\n");
    while (nl >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 1);
      dispatch(line, handlers);
      nl = buf.indexOf("\n");
    }
  });
  child.stdout?.on("end", () => {
    if (buf.length > 0) {
      dispatch(buf, handlers);
      buf = "";
    }
  });

  const exitPromise = new Promise<number | null>((resolve) => {
    child.once("exit", (code) => resolve(code));
  });

  return {
    onKey(handler) {
      handlers.add(handler);
      return () => {
        handlers.delete(handler);
      };
    },
    async close(): Promise<number | null> {
      if (child.exitCode === null && !child.killed) {
        child.kill("SIGINT");
      }
      return exitPromise;
    },
    wait(): Promise<number | null> {
      return exitPromise;
    },
  };
}

function dispatch(line: string, handlers: Set<(event: KeyInputEvent) => void>): void {
  if (line.trim().length === 0) return;
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return;
  }
  if (!isKeyEvent(parsed)) return;
  for (const handler of handlers) handler(parsed);
}

function isKeyEvent(value: unknown): value is KeyInputEvent {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  if (obj.event !== "key" || typeof obj.code !== "string") return false;
  if (obj.char !== undefined && typeof obj.char !== "string") return false;
  if (obj.modifiers !== undefined && !Array.isArray(obj.modifiers)) return false;
  return true;
}
