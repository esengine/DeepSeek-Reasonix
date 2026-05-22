import * as pty from "node-pty";

export interface PtyResult {
  exitCode: number | null;
  output: string;
  timedOut: boolean;
}

const INTERACTIVE_PREFIXES = ["sudo", "ssh", "mysql", "npm init", "npx create"];

export function needsPty(cmd: string): boolean {
  const trimmed = cmd.trim().toLowerCase();
  return INTERACTIVE_PREFIXES.some((c) => trimmed.startsWith(c));
}

export async function runWithPty(
  bin: string,
  args: string[],
  opts: {
    cwd: string;
    timeoutSec: number;
    maxOutputChars: number;
    env: NodeJS.ProcessEnv;
  },
): Promise<PtyResult> {
  return new Promise((resolve) => {
    const proc = pty.spawn(bin, args, {
      name: "xterm-256color",
      cols: process.stdout.columns ?? 80,
      rows: process.stdout.rows ?? 24,
      cwd: opts.cwd,
      env: opts.env as Record<string, string>,
    });

    let output = "";
    let timedOut = false;

    proc.onData((data) => {
      output += data;
      process.stdout.write(data);
      if (output.length > opts.maxOutputChars * 2) {
        output = output.slice(-opts.maxOutputChars);
      }
    });

    const stdinHandler = (data: Buffer) => proc.write(data.toString());
    process.stdin.setRawMode?.(true);
    process.stdin.resume();
    process.stdin.on("data", stdinHandler);

    const timer = setTimeout(() => {
      timedOut = true;
      proc.kill();
    }, opts.timeoutSec * 1000);

    proc.onExit(({ exitCode }) => {
      clearTimeout(timer);
      process.stdin.removeListener("data", stdinHandler);
      process.stdin.setRawMode?.(false);
      const trimmed =
        output.length > opts.maxOutputChars
          ? `${output.slice(0, opts.maxOutputChars)}\n\n[… truncated …]`
          : output;
      resolve({ exitCode: exitCode ?? null, output: trimmed, timedOut });
    });
  });
}
