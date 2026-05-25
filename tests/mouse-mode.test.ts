import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { disableMouseMode, enableMouseMode } from "../src/cli/ui/mouse-mode.js";

describe("mouse-mode enable/disable", () => {
  let writes: string[];
  let origWrite: typeof process.stdout.write;
  let origIsTTY: boolean | undefined;
  let origModeEnv: string | undefined;
  let origTermProgram: string | undefined;

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
    origTermProgram = process.env.TERM_PROGRAM;
    // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
    delete process.env.REASONIX_MOUSE_MODE;
    // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
    delete process.env.TERM_PROGRAM;
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
    if (origTermProgram === undefined) {
      // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
      delete process.env.TERM_PROGRAM;
    } else {
      process.env.TERM_PROGRAM = origTermProgram;
    }
  });

  it("default resets every mouse-capture mode so the terminal owns the wheel", () => {
    enableMouseMode();
    expect(writes.join("")).toBe(
      "\u001b[?9l\u001b[?1000l\u001b[?1002l\u001b[?1003l\u001b[?1005l\u001b[?1006l\u001b[?1007l\u001b[?1015l",
    );
  });

  it("default disable is a no-op — there's nothing for us to clean up", () => {
    enableMouseMode();
    writes.length = 0;
    disableMouseMode();
    expect(writes).toEqual([]);
  });

  it("does not send default mouse reset sequences to Apple Terminal", () => {
    process.env.TERM_PROGRAM = "Apple_Terminal";
    enableMouseMode();
    expect(writes).toEqual([]);
  });

  it("REASONIX_MOUSE_MODE=sgr forces ?1000h + ?1006h capture even off Windows", () => {
    process.env.REASONIX_MOUSE_MODE = "sgr";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1000h\u001b[?1006h");
    writes.length = 0;
    disableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1006l\u001b[?1000l");
  });

  it("app history scroll mode enables SGR mouse tracking by default", () => {
    enableMouseMode("app");
    expect(writes.join("")).toBe("\u001b[?1000h\u001b[?1006h");
  });

  it("REASONIX_MOUSE_MODE=alternate-scroll forces ?1007h even on Windows", () => {
    process.env.REASONIX_MOUSE_MODE = "alternate-scroll";
    enableMouseMode();
    expect(writes.join("")).toBe("\u001b[?1007h");
  });

  it("REASONIX_MOUSE_MODE=off resets every mouse-capture mode the terminal might be holding from a prior session", () => {
    process.env.REASONIX_MOUSE_MODE = "off";
    enableMouseMode();
    expect(writes.join("")).toBe(
      "\u001b[?9l\u001b[?1000l\u001b[?1002l\u001b[?1003l\u001b[?1005l\u001b[?1006l\u001b[?1007l\u001b[?1015l",
    );
    writes.length = 0;
    disableMouseMode();
    expect(writes).toEqual([]);
  });

  it("unknown REASONIX_MOUSE_MODE falls back to off (reset-all)", () => {
    process.env.REASONIX_MOUSE_MODE = "garbage";
    enableMouseMode();
    expect(writes.join("")).toBe(
      "\u001b[?9l\u001b[?1000l\u001b[?1002l\u001b[?1003l\u001b[?1005l\u001b[?1006l\u001b[?1007l\u001b[?1015l",
    );
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
