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

  it("default mode writes the alternate-scroll escape (?1007h)", () => {
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1007h");
  });

  it("default disable writes the matching off-escape (?1007l)", () => {
    enableMouseMode();
    writes.length = 0;
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1007l");
  });

  it("REASONIX_MOUSE_MODE=sgr restores legacy ?1000h + ?1006h capture", () => {
    process.env.REASONIX_MOUSE_MODE = "sgr";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1000h\u001b[?1006h");
    writes.length = 0;
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1006l\u001b[?1000l");
  });

  it("REASONIX_MOUSE_MODE=off skips writing any escape sequence", () => {
    process.env.REASONIX_MOUSE_MODE = "off";
    enableMouseMode();
    disableMouseMode();
    expect(writes).toEqual([]);
  });

  it("unknown REASONIX_MOUSE_MODE falls back to alternate-scroll default", () => {
    process.env.REASONIX_MOUSE_MODE = "garbage";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1007h");
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
