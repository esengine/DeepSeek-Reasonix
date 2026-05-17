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

  child.on("error", (err) => {
    process.stderr.write(`[spawnRenderer] child error: ${err.message}\n`);
  });

  let exited = false;
  let aliveMs = 0;
  const spawnedAt = Date.now();
  const exitPromise = new Promise<number | null>((resolve) => {
    child.once("exit", (code, signal) => {
      exited = true;
      aliveMs = Date.now() - spawnedAt;
      process.stderr.write(
        `[spawnRenderer] child exit: code=${code} signal=${signal} aliveMs=${aliveMs}\n`,
      );
      resolve(code);
      // Only propagate exit event AFTER the child has been alive long enough
      // that this is plausibly an "intentional exit" (user pressed Ctrl+D,
      // /exit slash, etc.). A sub-second death = startup crash; synthesizing
      // exit there would cascade Node into immediately killing itself and
      // mask the real error. Let Node stay alive so the user can grep the
      // stderr log file for what the rust child wrote before dying.
      if (opts.integrated && opts.onEvent && aliveMs >= 1500) {
        try {
          opts.onEvent({ event: "exit" });
        } catch {
          // handler errors mustn't block the close pipeline
        }
      }
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
            continue;
          }
        } catch {
          // not JSON; fall through to log as raw stderr
        }
        // Surface non-event stderr lines (rust panics, debug prints, anything
        // crossterm spat out before alt-screen took over). Previously silently
        // dropped — making "rust child dies immediately" undebuggable.
        process.stderr.write(`[rust-stderr] ${line}\n`);
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
