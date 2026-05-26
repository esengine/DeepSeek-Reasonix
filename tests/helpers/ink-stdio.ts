import { EventEmitter } from "node:events";
import stripAnsi from "strip-ansi";

/** Stdin shim for Ink 7's useInput raw-mode check; CI's process.stdin isn't a TTY. ink-testing-library covers this but pins stdout columns to 100 with no override — tests asserting layout width need 120. */
export function makeFakeStdin() {
  const ee = new EventEmitter() as EventEmitter & Record<string, unknown>;
  ee.isTTY = true;
  ee.setEncoding = () => {};
  ee.setRawMode = () => ee;
  ee.resume = () => ee;
  ee.pause = () => ee;
  ee.ref = () => {};
  ee.unref = () => {};
  ee.read = () => null;
  ee.isRawModeSupported = true;
  return ee;
}

// biome-ignore lint/suspicious/noControlCharactersInRegex: CSI N C is exactly what we're matching to restore visual gaps the new ink renderer encodes as cursor-forward instead of literal spaces.
const CURSOR_FORWARD = /\x1b\[(\d*)C/g;

function normalize(raw: string): string {
  const widened = raw.replace(CURSOR_FORWARD, (_m, n) => " ".repeat(Number(n) || 1));
  return stripAnsi(widened);
}

/** Captures Ink writes; .text() returns text with cursor-forward gaps re-expanded to spaces and all other ANSI stripped. Use .raw() when assertions need the bytes (e.g. OSC-8 emission). */
export function makeFakeStdout() {
  const chunks: string[] = [];
  return {
    columns: 120,
    rows: 30,
    isTTY: true,
    write(chunk: string) {
      chunks.push(chunk);
      return true;
    },
    on() {},
    off() {},
    text(): string {
      return normalize(chunks.join(""));
    },
    raw(): string {
      return chunks.join("");
    },
  };
}
