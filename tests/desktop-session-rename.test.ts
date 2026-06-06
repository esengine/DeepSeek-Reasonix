import { describe, expect, it } from "vitest";
import { normalizeSessionTitle } from "../src/cli/commands/desktop.js";

describe("normalizeSessionTitle", () => {
  it("trims surrounding whitespace and collapses internal runs", () => {
    expect(normalizeSessionTitle("  hello   world  ")).toBe("hello world");
  });

  it("returns empty string for whitespace-only input — caller treats as clear", () => {
    expect(normalizeSessionTitle("   \n\t  ")).toBe("");
  });

  it("caps at 200 characters", () => {
    const long = "x".repeat(500);
    expect(normalizeSessionTitle(long)).toHaveLength(200);
  });

  it("preserves CJK without truncating below the cap", () => {
    expect(normalizeSessionTitle("我的会话")).toBe("我的会话");
  });
});
