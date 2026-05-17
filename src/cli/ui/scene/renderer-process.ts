import { spawn } from "node:child_process";

export type RustEvent =
  | { event: "submit"; text: string }
  | { event: "interrupt" }
  | { event: "exit" }
  | { event: "approval-response"; kind: string; choice: unknown }
  | { event: "composer"; text: string }
  | { event: "mode-set"; value: "review" | "auto" | "yolo" }
  | { event: "preset-set"; value: "auto" | "flash" | "pro" }
  | { event: "prompt-response"; id: string; text?: string; cancelled?: boolean }
  | { event: "list-picker-response"; id: string; key?: string; cancelled?: boolean };

export type RendererProcess = {
  emit(message: unknown): void;
  close(): Promise<number | null>;
};

export type SpawnRendererOptions = {
  command: readonly string[];
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  /** When true the rust child owns keyboard + composer; its stderr is parsed for {event:"submit"|"interrupt"|"exit"} lines. */
  integrated?: boolean;
  /** Called for each event line from the rust child's stderr. Only meaningful when `integrated` is true. */
  onEvent?: (event: RustEvent) => void;
};

export function spawnRenderer(opts: SpawnRendererOptions): RendererProcess {
  const command = opts.command;
  const baseArgs: string[] = [];
  const [cmd, ...rest] = command;
  baseArgs.push(...rest);
  if (opts.integrated) {
    baseArgs.push("--integrated");
  }
  if (!cmd) {
    throw new Error("spawnRenderer: empty command");
  }

  const stderrStdio: "inherit" | "pipe" = opts.integrated ? "pipe" : "inherit";
  const child = spawn(cmd, baseArgs, {
    cwd: opts.cwd,
    env: opts.env ?? process.env,
    stdio: ["pipe", "inherit", stderrStdio],
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

  if (opts.integrated && opts.onEvent && child.stderr) {
    let buf = "";
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => {
      buf += chunk;
      for (;;) {
        const nl = buf.indexOf("\n");
        if (nl === -1) break;
        const line = buf.slice(0, nl).trim();
        buf = buf.slice(nl + 1);
        if (line.length === 0) continue;
        try {
          const parsed = JSON.parse(line) as RustEvent;
          if (parsed && typeof parsed.event === "string") {
            opts.onEvent?.(parsed);
          }
        } catch {
          // ignore non-JSON stderr lines (panic output, debug, etc.)
        }
      }
    });
  }

  return {
    emit(message: unknown): void {
      if (exited) return;
      const stdin = child.stdin;
      if (!stdin || stdin.destroyed || !stdin.writable) return;
      stdin.write(`${JSON.stringify(message)}\n`);
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
