import { describe, expect, it } from "vitest";
import { bgCode, fgCode } from "../../../src/renderer/ink-compat/colors.js";

describe("fgCode — named colors", () => {
  it("cyan → SGR 36 with 39 revert", () => {
    expect(fgCode("cyan")).toEqual({ apply: "\x1b[36m", revert: "\x1b[39m" });
  });
  it("gray and grey both map to 90", () => {
    expect(fgCode("gray")?.apply).toBe("\x1b[90m");
    expect(fgCode("grey")?.apply).toBe("\x1b[90m");
  });
  it("redBright maps to 91", () => {
    expect(fgCode("redBright")?.apply).toBe("\x1b[91m");
  });
  it("unknown color returns null", () => {
    expect(fgCode("plaid")).toBeNull();
  });
  it("undefined returns null", () => {
    expect(fgCode(undefined)).toBeNull();
  });
});

describe("fgCode — hex colors", () => {
  it("#ff8800 → 24-bit fg", () => {
    expect(fgCode("#ff8800")).toEqual({
      apply: "\x1b[38;2;255;136;0m",
      revert: "\x1b[39m",
    });
  });
  it("#fa0 short form expands", () => {
    expect(fgCode("#fa0")?.apply).toBe("\x1b[38;2;255;170;0m");
  });
});

describe("bgCode — named colors", () => {
  it("blue → SGR 44", () => {
    expect(bgCode("blue")).toEqual({ apply: "\x1b[44m", revert: "\x1b[49m" });
  });
  it("gray → 100", () => {
    expect(bgCode("gray")?.apply).toBe("\x1b[100m");
  });
});

describe("bgCode — hex", () => {
  it("#001122 → 24-bit bg", () => {
    expect(bgCode("#001122")?.apply).toBe("\x1b[48;2;0;17;34m");
  });
});
