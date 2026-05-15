import { spawn } from "node:child_process";
import { DEFAULT_COMMAND } from "./renderer-process.js";

export type InputModifier = "ctrl" | "alt" | "shift" | "super";

export type KeyInputEvent = {
  event: "key";
  code: string;
  char?: string;
  modifiers?: readonly InputModifier[];
};

export type PasteInputEvent = {
  event: "paste";
  text: string;
};

export type InputSource = {
  onKey(handler: (event: KeyInputEvent) => void): () => void;
  onPaste(handler: (event: PasteInputEvent) => void): () => void;
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

type Buses = {
  keys: Set<(event: KeyInputEvent) => void>;
  pastes: Set<(event: PasteInputEvent) => void>;
};

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

  const buses: Buses = { keys: new Set(), pastes: new Set() };
  let buf = "";

  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => {
    buf += chunk;
    let nl = buf.indexOf("\n");
    while (nl >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 1);
      dispatch(line, buses);
      nl = buf.indexOf("\n");
    }
  });
  child.stdout?.on("end", () => {
    if (buf.length > 0) {
      dispatch(buf, buses);
      buf = "";
    }
  });

  const exitPromise = new Promise<number | null>((resolve) => {
    child.once("exit", (code) => resolve(code));
  });

  return {
    onKey(handler) {
      buses.keys.add(handler);
      return () => {
        buses.keys.delete(handler);
      };
    },
    onPaste(handler) {
      buses.pastes.add(handler);
      return () => {
        buses.pastes.delete(handler);
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

function dispatch(line: string, buses: Buses): void {
  if (line.trim().length === 0) return;
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return;
  }
  if (isKeyEvent(parsed)) {
    for (const handler of buses.keys) handler(parsed);
    return;
  }
  if (isPasteEvent(parsed)) {
    for (const handler of buses.pastes) handler(parsed);
    return;
  }
}

function isKeyEvent(value: unknown): value is KeyInputEvent {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  if (obj.event !== "key" || typeof obj.code !== "string") return false;
  if (obj.char !== undefined && typeof obj.char !== "string") return false;
  if (obj.modifiers !== undefined && !Array.isArray(obj.modifiers)) return false;
  return true;
}

function isPasteEvent(value: unknown): value is PasteInputEvent {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  return obj.event === "paste" && typeof obj.text === "string";
}
