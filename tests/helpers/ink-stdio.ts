import { EventEmitter } from "node:events";

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

/** Captures Ink writes; .text() returns ANSI-SGR-stripped output at fixed 120×30. */
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
      // biome-ignore lint/suspicious/noControlCharactersInRegex: stripping ANSI SGR codes
      return chunks.join("").replace(/\x1b\[[0-9;]*m/g, "");
    },
  };
}
