/** Parse + spawn `cmd1 | cmd2 && cmd3` ourselves — never invoke a shell, sidestep PS5.1's `&&` parse error. */

import { type ChildProcess, type SpawnOptions, spawn } from "node:child_process";
import {
  detectShellOperator,
  killProcessTree,
  prepareSpawn,
  smartDecodeOutput,
  tokenizeCommand,
} from "./shell.js";

export type ChainOp = "|" | "||" | "&&" | ";";

export interface ChainSegment {
  argv: string[];
}

export interface CommandChain {
  segments: ChainSegment[];
  /** length === segments.length - 1 */
  ops: ChainOp[];
}

export class UnsupportedSyntaxError extends Error {
  constructor(detail: string) {
    super(`run_command: ${detail}`);
    this.name = "UnsupportedSyntaxError";
  }
}

/** Whitespace-bounded splitter — chain ops only count when they begin a token, so `--flag=1&2` stays literal. */
function splitOnChainOps(cmd: string): { segs: string[]; ops: ChainOp[] } {
  const segs: string[] = [];
  const ops: ChainOp[] = [];
  let segStart = 0;
  let i = 0;
  let quote: '"' | "'" | null = null;
  let atTokenStart = true;
  while (i < cmd.length) {
    const ch = cmd[i]!;
    if (quote) {
      if (ch === quote) quote = null;
      else if (ch === "\\" && quote === '"' && i + 1 < cmd.length) i++;
      i++;
      atTokenStart = false;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      i++;
      atTokenStart = false;
      continue;
    }
    if (ch === " " || ch === "\t") {
      i++;
      atTokenStart = true;
      continue;
    }
    if (atTokenStart) {
      let op: ChainOp | null = null;
      let opLen = 0;
      const next = cmd[i + 1];
      if (ch === "|" && next === "|") {
        op = "||";
        opLen = 2;
      } else if (ch === "&" && next === "&") {
        op = "&&";
        opLen = 2;
      } else if (ch === "|") {
        op = "|";
        opLen = 1;
      } else if (ch === ";") {
        op = ";";
        opLen = 1;
      }
      if (op !== null) {
        segs.push(cmd.slice(segStart, i));
        ops.push(op);
        i += opLen;
        segStart = i;
        atTokenStart = true;
        continue;
      }
    }
    i++;
    atTokenStart = false;
  }
  segs.push(cmd.slice(segStart));
  return { segs, ops };
}

/** Returns null on plain commands (caller takes the simple path); throws on unsupported syntax inside any segment. */
export function parseCommandChain(cmd: string): CommandChain | null {
  const { segs, ops } = splitOnChainOps(cmd);
  if (ops.length === 0) return null;
  const segments: ChainSegment[] = [];
  for (let i = 0; i < segs.length; i++) {
    const trimmed = segs[i]!.trim();
    if (trimmed.length === 0) {
      const op = i === 0 ? ops[0]! : ops[i - 1]!;
      throw new UnsupportedSyntaxError(
        i === 0
          ? `empty segment before "${op}"`
          : i === segs.length - 1
            ? `chain ends with "${op}"`
            : `empty segment between "${ops[i - 1]}" and "${ops[i]}"`,
      );
    }
    const segOp = detectShellOperator(trimmed);
    if (segOp !== null) {
      throw new UnsupportedSyntaxError(
        `shell operator "${segOp}" is not supported — only \`|\`, \`||\`, \`&&\`, \`;\` chain operators are spawned natively. Redirects (\`>\`, \`<\`, \`2>&1\`) and background (\`&\`) require splitting into separate run_command calls.`,
      );
    }
    segments.push({ argv: tokenizeCommand(trimmed) });
  }
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
