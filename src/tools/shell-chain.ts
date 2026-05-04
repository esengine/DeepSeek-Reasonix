/** Parse + spawn `cmd1 | cmd2 && cmd3` ourselves — never invoke a shell, sidestep PS5.1's `&&` parse error. */

import { type ChildProcess, type SpawnOptions, spawn } from "node:child_process";
import { parse as shellParse } from "shell-quote";
import { killProcessTree, prepareSpawn, smartDecodeOutput } from "./shell.js";

export type ChainOp = "|" | "||" | "&&" | ";";

export interface ChainSegment {
  argv: string[];
}

export interface CommandChain {
  segments: ChainSegment[];
  /** length === segments.length - 1 */
  ops: ChainOp[];
}

const CHAIN_OPS = new Set<string>(["|", "||", "&&", ";"]);

export class UnsupportedSyntaxError extends Error {
  constructor(detail: string) {
    super(`run_command: ${detail}`);
    this.name = "UnsupportedSyntaxError";
  }
}

/** Returns null on plain commands (caller takes the simple path); throws on unsupported syntax. */
export function parseCommandChain(cmd: string): CommandChain | null {
  // shell-quote calls env() with name="" for `$(...)` — defer that to the `(` op handler.
  const tokens = shellParse(cmd, (name: string) =>
    name === "" ? "$" : { op: "$VAR" as const, name },
  );
  const segments: ChainSegment[] = [];
  const ops: ChainOp[] = [];
  let cur: string[] = [];
  let sawChainOp = false;
  for (const t of tokens) {
    if (typeof t === "string") {
      cur.push(t);
      continue;
    }
    if ("comment" in t) continue;
    const op = (t as { op: string }).op;
    if (CHAIN_OPS.has(op)) {
      sawChainOp = true;
      if (cur.length === 0) throw new UnsupportedSyntaxError(`empty segment before "${op}"`);
      segments.push({ argv: cur });
      ops.push(op as ChainOp);
      cur = [];
      continue;
    }
    if (op === "glob") {
      cur.push((t as { pattern: string }).pattern);
      continue;
    }
    if (op === "$VAR") {
      const name = (t as { name: string }).name;
      throw new UnsupportedSyntaxError(
        `\$${name} expansion is not supported — pass values as literals, or use the binary's own --env flag`,
      );
    }
    if (op === "(" || op === ")") {
      throw new UnsupportedSyntaxError(
        "command substitution / subshells are not supported — split into separate calls",
      );
    }
    throw new UnsupportedSyntaxError(
      `shell operator "${op}" is not supported — only \`|\`, \`||\`, \`&&\`, \`;\` chain operators work; redirects (\`>\`, \`<\`, \`2>&1\`) are rejected`,
    );
  }
  if (!sawChainOp) return null;
  if (cur.length === 0) {
    throw new UnsupportedSyntaxError(`chain ends with "${ops[ops.length - 1]}"`);
  }
  segments.push({ argv: cur });
  return { segments, ops };
}

/** Each segment must individually clear the allowlist for the chain to auto-run. */
export function chainAllowed(
  chain: CommandChain,
  isAllowed: (segmentCmd: string) => boolean,
): boolean {
  for (const seg of chain.segments) {
    if (!isAllowed(seg.argv.join(" "))) return false;
  }
  return true;
}

export interface ChainResult {
  exitCode: number | null;
  output: string;
  timedOut: boolean;
}

interface ChainGroup {
  segments: ChainSegment[];
  /** Op connecting the PREVIOUS group to THIS one (`||`, `&&`, `;`); null on the first group. */
  opBefore: Exclude<ChainOp, "|"> | null;
}

/** Pipe groups are runs of segments joined by `|`; sequential ops (`||`, `&&`, `;`) split them. */
function groupChain(chain: CommandChain): ChainGroup[] {
  const groups: ChainGroup[] = [{ segments: [chain.segments[0]!], opBefore: null }];
  for (let i = 0; i < chain.ops.length; i++) {
    const op = chain.ops[i]!;
    const next = chain.segments[i + 1]!;
    if (op === "|") {
      groups[groups.length - 1]!.segments.push(next);
    } else {
      groups.push({ segments: [next], opBefore: op });
    }
  }
  return groups;
}

export interface RunChainOptions {
  cwd: string;
  timeoutSec: number;
  maxOutputChars: number;
  signal?: AbortSignal;
}

export async function runChain(chain: CommandChain, opts: RunChainOptions): Promise<ChainResult> {
  const groups = groupChain(chain);
  const buf = new OutputBuffer(opts.maxOutputChars * 2 * 4);
  const deadline = Date.now() + opts.timeoutSec * 1000;
  let lastExit: number | null = 0;
  let timedOut = false;
  for (const group of groups) {
    if (group.opBefore === "&&" && lastExit !== 0) continue;
    if (group.opBefore === "||" && lastExit === 0) continue;
    const remainingMs = deadline - Date.now();
    if (remainingMs <= 0) {
      timedOut = true;
      break;
    }
    const result = await runPipeGroup(group.segments, {
      cwd: opts.cwd,
      timeoutMs: remainingMs,
      buf,
      signal: opts.signal,
    });
    lastExit = result.exitCode;
    if (result.timedOut) {
      timedOut = true;
      break;
    }
    if (opts.signal?.aborted) break;
  }
  const output = buf.toString();
  const truncated =
    output.length > opts.maxOutputChars
      ? `${output.slice(0, opts.maxOutputChars)}\n\n[… truncated ${output.length - opts.maxOutputChars} chars …]`
      : output;
  return { exitCode: lastExit, output: truncated, timedOut };
}

interface PipeGroupResult {
  exitCode: number | null;
  timedOut: boolean;
}

interface PipeGroupOptions {
  cwd: string;
  timeoutMs: number;
  buf: OutputBuffer;
  signal?: AbortSignal;
}

async function runPipeGroup(
  segments: ChainSegment[],
  opts: PipeGroupOptions,
): Promise<PipeGroupResult> {
  const env = { ...process.env, PYTHONIOENCODING: "utf-8", PYTHONUTF8: "1" };
  const children: ChildProcess[] = [];
  let timedOut = false;
  const killAll = () => {
    for (const c of children) killProcessTree(c);
  };
  const killTimer = setTimeout(() => {
    timedOut = true;
    killAll();
  }, opts.timeoutMs);
  const onAbort = () => killAll();
  if (opts.signal?.aborted) {
    onAbort();
  } else {
    opts.signal?.addEventListener("abort", onAbort, { once: true });
  }
  try {
    for (let i = 0; i < segments.length; i++) {
      const isFirst = i === 0;
      const isLast = i === segments.length - 1;
      const { bin, args, spawnOverrides } = prepareSpawn(segments[i]!.argv);
      const spawnOpts: SpawnOptions = {
        cwd: opts.cwd,
        shell: false,
        windowsHide: true,
        env,
        stdio: [isFirst ? "ignore" : "pipe", isLast ? "pipe" : "pipe", "pipe"],
        ...spawnOverrides,
      };
      let child: ChildProcess;
      try {
        child = spawn(bin, args, spawnOpts);
      } catch (err) {
        killAll();
        clearTimeout(killTimer);
        opts.signal?.removeEventListener("abort", onAbort);
        throw err;
      }
      children.push(child);
      if (!isFirst) {
        const prev = children[i - 1]!;
        prev.stdout?.on("error", () => {});
        child.stdin?.on("error", () => {});
        prev.stdout?.pipe(child.stdin!);
      }
      child.stderr?.on("data", (chunk: Buffer | string) => opts.buf.push(toBuf(chunk)));
      if (isLast) {
        child.stdout?.on("data", (chunk: Buffer | string) => opts.buf.push(toBuf(chunk)));
      }
    }
    const exits = await Promise.all(
      children.map(
        (c) =>
          new Promise<number | null>((resolve) => {
            c.once("error", () => resolve(null));
            c.once("close", (code) => resolve(code));
          }),
      ),
    );
    return { exitCode: exits[exits.length - 1] ?? null, timedOut };
  } finally {
    clearTimeout(killTimer);
    opts.signal?.removeEventListener("abort", onAbort);
  }
}

function toBuf(chunk: Buffer | string): Buffer {
  return typeof chunk === "string" ? Buffer.from(chunk) : chunk;
}

class OutputBuffer {
  private chunks: Buffer[] = [];
  private bytes = 0;
  constructor(private readonly cap: number) {}
  push(b: Buffer): void {
    if (this.bytes >= this.cap) return;
    const remaining = this.cap - this.bytes;
    if (b.length > remaining) {
      this.chunks.push(b.subarray(0, remaining));
      this.bytes = this.cap;
    } else {
      this.chunks.push(b);
      this.bytes += b.length;
    }
  }
  toString(): string {
    return smartDecodeOutput(Buffer.concat(this.chunks));
  }
}
