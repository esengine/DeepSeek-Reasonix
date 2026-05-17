import type { KeystrokeReader } from "../keystroke-context.js";

/** Rust owns the TTY; Node calling setRawMode on process.stdin would race it on Windows. */
export const nullKeystrokeReader: KeystrokeReader = {
  start() {},
  subscribe() {
    return () => {};
  },
};
