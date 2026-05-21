import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { disableMouseMode, enableMouseMode } from "../src/cli/ui/mouse-mode.js";

describe("mouse-mode enable/disable", () => {
  let writes: string[];
  let origWrite: typeof process.stdout.write;
  let origIsTTY: boolean | undefined;
  let origModeEnv: string | undefined;

  beforeEach(() => {
    writes = [];
    origWrite = process.stdout.write.bind(process.stdout);
    process.stdout.write = ((chunk: string | Uint8Array) => {
      writes.push(typeof chunk === "string" ? chunk : Buffer.from(chunk).toString());
      return true;
    }) as typeof process.stdout.write;
    origIsTTY = process.stdout.isTTY;
    Object.defineProperty(process.stdout, "isTTY", { value: true, configurable: true });
    origModeEnv = process.env.REASONIX_MOUSE_MODE;
    // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
    delete process.env.REASONIX_MOUSE_MODE;
    // Reset module state — disable first to clear `active` from any prior test.
    disableMouseMode();
    writes.length = 0;
  });

  afterEach(() => {
    disableMouseMode();
    process.stdout.write = origWrite;
    Object.defineProperty(process.stdout, "isTTY", { value: origIsTTY, configurable: true });
    if (origModeEnv === undefined) {
      // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
      delete process.env.REASONIX_MOUSE_MODE;
    } else {
      process.env.REASONIX_MOUSE_MODE = origModeEnv;
    }
  });

  it("default picks the platform-appropriate protocol (SGR on Windows, alternate-scroll elsewhere)", () => {
    enableMouseMode();
    const expected = process.platform === "win32" ? "\u001b[?1000h\u001b[?1006h" : "\u001b[?1007h";
    expect(writes.join("")).toBe(expected);
  });

  it("default disable matches the enable sequence on the current platform", () => {
    enableMouseMode();
    writes.length = 0;
    disableMouseMode();
    const expected = process.platform === "win32" ? "\u001b[?1006l\u001b[?1000l" : "\u001b[?1007l";
    expect(writes.join("")).toBe(expected);
  });

  it("REASONIX_MOUSE_MODE=sgr forces ?1000h + ?1006h capture even off Windows", () => {
    process.env.REASONIX_MOUSE_MODE = "sgr";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1000h\u001b[?1006h");
    writes.length = 0;
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1006l\u001b[?1000l");
  });

  it("REASONIX_MOUSE_MODE=alternate-scroll forces ?1007h even on Windows", () => {
    process.env.REASONIX_MOUSE_MODE = "alternate-scroll";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1007h");
  });

  it("REASONIX_MOUSE_MODE=off skips writing any escape sequence", () => {
    process.env.REASONIX_MOUSE_MODE = "off";
    enableMouseMode();
    disableMouseMode();
    expect(writes).toEqual([]);
  });

  it("unknown REASONIX_MOUSE_MODE falls back to the platform default", () => {
    process.env.REASONIX_MOUSE_MODE = "garbage";
    enableMouseMode();
    const expected = process.platform === "win32" ? "\u001b[?1000h\u001b[?1006h" : "\u001b[?1007h";
    expect(writes.join("")).toBe(expected);
  });

  it("enable is idempotent — second call is a no-op", () => {
    enableMouseMode();
    enableMouseMode();
    expect(writes.length).toBe(1);
  });

  it("disable without prior enable is a no-op", () => {
    disableMouseMode();
    expect(writes.length).toBe(0);
  });

  it("disable uses the mode active at enable time, not the current env", () => {
    // Switching env after enable mustn't desync the disable sequence — it
    // would leave the terminal stuck in a half-set state.
    process.env.REASONIX_MOUSE_MODE = "sgr";
    enableMouseMode();
    writes.length = 0;
    process.env.REASONIX_MOUSE_MODE = "alternate-scroll";
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1006l\u001b[?1000l");
  });

  it("enable when stdout isn't a TTY is a no-op", () => {
    Object.defineProperty(process.stdout, "isTTY", { value: false, configurable: true });
    enableMouseMode();
    expect(writes.length).toBe(0);
    disableMouseMode();
    expect(writes.length).toBe(0);
  });
});
