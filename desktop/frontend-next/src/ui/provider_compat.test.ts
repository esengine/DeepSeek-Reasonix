import { describe, expect, it } from "vitest";
import { IDLE_TIMEOUT_MAX, IDLE_TIMEOUT_MIN, parseIdleTimeout } from "./provider_compat";

describe("parseIdleTimeout", () => {
  it("keeps the kernel's bounds", () => {
    expect([IDLE_TIMEOUT_MIN, IDLE_TIMEOUT_MAX]).toEqual([1, 32767]);
  });

  it("reads empty as the default, which the kernel stores as 0", () => {
    expect(parseIdleTimeout("")).toEqual({ ok: true, secs: 0 });
    expect(parseIdleTimeout("  ")).toEqual({ ok: true, secs: 0 });
  });

  it("accepts the bounds and what lies between", () => {
    expect(parseIdleTimeout("1")).toEqual({ ok: true, secs: 1 });
    expect(parseIdleTimeout("300")).toEqual({ ok: true, secs: 300 });
    expect(parseIdleTimeout("32767")).toEqual({ ok: true, secs: 32767 });
  });

  it("refuses what the kernel would", () => {
    for (const bad of ["0", "32768", "1.5", "-3", "abc", "1e3", "12 s", "00"]) {
      expect(parseIdleTimeout(bad), bad).toEqual({ ok: false });
    }
  });
});
