import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { disableMouseMode, enableMouseMode } from "../src/cli/ui/mouse-mode.js";

describe("mouse-mode SGR enable/disable", () => {
  let writes: string[];
  let origWrite: typeof process.stdout.write;
  let origIsTTY: boolean | undefined;

  beforeEach(() => {
    writes = [];
    origWrite = process.stdout.write.bind(process.stdout);
    process.stdout.write = ((chunk: string | Uint8Array) => {
      writes.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString());
      return true;
    }) as typeof process.stdout.write;
    origIsTTY = process.stdout.isTTY;
    Object.defineProperty(process.stdout, "isTTY", { value: true, configurable: true });
    // Reset module state — disable first to clear `active` from any prior test.
    disableMouseMode();
    writes.length = 0;
  });

  afterEach(() => {
    disableMouseMode();
    process.stdout.write = origWrite;
    Object.defineProperty(process.stdout, "isTTY", { value: origIsTTY, configurable: true });
  });

  it("enable writes the SGR + 1000 + 1006 escape", () => {
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1000h\u001b[?1006h");
  });

  it("enable is idempotent — second call is a no-op", () => {
    enableMouseMode();
    enableMouseMode();
    expect(writes.length).toBe(1);
  });

  it("disable writes the matching off-escape", () => {
    enableMouseMode();
    writes.length = 0;
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1006l\u001b[?1000l");
  });

  it("disable without prior enable is a no-op", () => {
    disableMouseMode();
    expect(writes.length).toBe(0);
  });

  it("enable when stdout isn't a TTY is a no-op", () => {
    Object.defineProperty(process.stdout, "isTTY", { value: false, configurable: true });
    enableMouseMode();
    expect(writes.length).toBe(0);
    // And subsequent disable is also a no-op (active flag never flipped).
    disableMouseMode();
    expect(writes.length).toBe(0);
  });
});
