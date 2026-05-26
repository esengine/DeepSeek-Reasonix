import { EventEmitter } from "node:events";
import { render as inkRender } from "ink";
import stripAnsi from "strip-ansi";

// biome-ignore lint/suspicious/noControlCharactersInRegex: CSI N C restores visual gaps the renderer encodes as cursor-forward instead of literal spaces.
const CURSOR_FORWARD = /\x1b\[(\d*)C/g;

function normalize(raw: string): string {
  const widened = raw.replace(CURSOR_FORWARD, (_m, n) => " ".repeat(Number(n) || 1));
  return stripAnsi(widened);
}

class TestStdout extends EventEmitter {
  columns = 100;
  rows = 30;
  isTTY = true;
  chunks: string[] = [];
  write = (chunk: string): boolean => {
    this.chunks.push(chunk);
    return true;
  };
  get(): string {
    return this.chunks.join("");
  }
}

class TestStdin extends EventEmitter {
  isTTY = true;
  data: string | null = null;
  constructor(opts: { isTTY?: boolean } = {}) {
    super();
    if (opts.isTTY !== undefined) this.isTTY = opts.isTTY;
  }
  write = (data: string): void => {
    this.data = data;
    this.emit("readable");
    this.emit("data", data);
  };
  setEncoding = (): void => {};
  setRawMode = (): TestStdin => this;
  resume = (): TestStdin => this;
  pause = (): TestStdin => this;
  ref = (): void => {};
  unref = (): void => {};
  read = (): string | null => {
    const d = this.data;
    this.data = null;
    return d;
  };
}

export interface RenderResult {
  lastFrame: () => string;
  /** Raw concatenated bytes with all ANSI/escape sequences intact — for tests asserting on OSC-8 hyperlinks or other protocol-level emissions. */
  lastFrameRaw: () => string;
  rawFrames: () => string[];
  rerender: (tree: React.ReactNode) => void;
  unmount: () => void;
  stdin: TestStdin;
  cleanup: () => void;
}

export function render(tree: React.ReactNode): RenderResult {
  const stdout = new TestStdout();
  const stdin = new TestStdin();
  const stderr = new TestStdout();
  const instance = inkRender(tree, {
    stdout: stdout as unknown as NodeJS.WriteStream,
    stdin: stdin as unknown as NodeJS.ReadStream,
    stderr: stderr as unknown as NodeJS.WriteStream,
    exitOnCtrlC: false,
    patchConsole: false,
  });
  return {
    lastFrame: () => normalize(stdout.get()),
    lastFrameRaw: () => stdout.get(),
    rawFrames: () => stdout.chunks,
    rerender: instance.rerender,
    unmount: () => instance.unmount(),
    stdin,
    cleanup: () => instance.cleanup?.(),
  };
}
