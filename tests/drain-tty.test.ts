import { describe, expect, it } from "vitest";
import { drainTtyResponses } from "../src/cli/ui/drain-tty.js";

describe("drainTtyResponses (#365)", () => {
  it("returns immediately on non-TTY stdin without throwing", async () => {
    const start = Date.now();
    await drainTtyResponses(50);
    const elapsed = Date.now() - start;
    expect(elapsed).toBeLessThan(40);
  });

  it("respects the timeout when raw mode IS available (caps total wait)", async () => {
    const fakeStdin = makeFakeRawStdin();
    const orig = process.stdin;
    Object.defineProperty(process, "stdin", { value: fakeStdin, configurable: true });
    try {
      const start = Date.now();
      await drainTtyResponses(30);
      const elapsed = Date.now() - start;
      expect(elapsed).toBeGreaterThanOrEqual(25);
      expect(elapsed).toBeLessThan(150);
      expect(fakeStdin.rawModeOn).toBe(false);
    } finally {
      Object.defineProperty(process, "stdin", { value: orig, configurable: true });
    }
  });

  it("discards bytes the terminal pushed during the drain window", async () => {
    const fakeStdin = makeFakeRawStdin();
    const orig = process.stdin;
    Object.defineProperty(process, "stdin", { value: fakeStdin, configurable: true });
    try {
      const drainPromise = drainTtyResponses(40);
      setTimeout(() => fakeStdin.push(Buffer.from("\x1b]11;rgb:1111/2222/3333\x1b\\")), 5);
      setTimeout(() => fakeStdin.push(Buffer.from("\x1b[33;1R\x1b[?62;1;4c")), 15);
      await drainPromise;
      // No assertion on what was discarded — the drain just has to not blow up
      // when a terminal-response burst arrives mid-window.
      expect(fakeStdin.rawModeOn).toBe(false);
    } finally {
      Object.defineProperty(process, "stdin", { value: orig, configurable: true });
    }
  });
});

function makeFakeRawStdin(): {
  isTTY: true;
  rawModeOn: boolean;
  setRawMode: (on: boolean) => void;
  resume: () => void;
  pause: () => void;
  on: (ev: string, fn: (chunk: Buffer | string) => void) => void;
  off: (ev: string, fn: (chunk: Buffer | string) => void) => void;
  push: (chunk: Buffer) => void;
} {
  const listeners: Array<(c: Buffer | string) => void> = [];
  return {
    isTTY: true,
    rawModeOn: false,
    setRawMode(on: boolean): void {
      this.rawModeOn = on;
    },
    resume(): void {},
    pause(): void {},
    on(ev: string, fn: (c: Buffer | string) => void): void {
      if (ev === "data") listeners.push(fn);
    },
    off(ev: string, fn: (c: Buffer | string) => void): void {
      if (ev === "data") {
        const i = listeners.indexOf(fn);
        if (i >= 0) listeners.splice(i, 1);
      }
    },
    push(chunk: Buffer): void {
      for (const fn of [...listeners]) fn(chunk);
    },
  };
}
