import { spawn } from "node:child_process";
import type { SceneFrame } from "./types.js";

export type RendererProcess = {
  emit(frame: SceneFrame): void;
  close(): Promise<number | null>;
};

export type SpawnRendererOptions = {
  command?: readonly string[];
  cwd?: string;
  env?: NodeJS.ProcessEnv;
};

export const DEFAULT_COMMAND: readonly string[] = [
  "cargo",
  "run",
  "--quiet",
  "--bin",
  "reasonix-render",
];

export function spawnRenderer(opts: SpawnRendererOptions = {}): RendererProcess {
  const command = opts.command ?? DEFAULT_COMMAND;
  const [cmd, ...args] = command;
  if (!cmd) {
    throw new Error("spawnRenderer: empty command");
  }

  const child = spawn(cmd, args, {
    cwd: opts.cwd,
    env: opts.env ?? process.env,
    stdio: ["pipe", "inherit", "inherit"],
  });

  let exited = false;
  const exitPromise = new Promise<number | null>((resolve) => {
    child.once("exit", (code) => {
      exited = true;
      resolve(code);
    });
  });

  child.stdin?.on("error", () => {
    exited = true;
  });

  return {
    emit(frame: SceneFrame): void {
      if (exited) return;
      const stdin = child.stdin;
      if (!stdin || stdin.destroyed || !stdin.writable) return;
      stdin.write(`${JSON.stringify(frame)}\n`);
    },
    close(): Promise<number | null> {
      const stdin = child.stdin;
      if (stdin && !stdin.destroyed && stdin.writable) {
        stdin.end();
      }
      return exitPromise;
    },
  };
}
