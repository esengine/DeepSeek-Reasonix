import { describe, expect, it } from "vitest";
import { largestStringInputBytes } from "../src/cli/ui/cards/ToolCard.js";

describe("largestStringInputBytes", () => {
  it("returns null for short string args (below 1 KB threshold)", () => {
    expect(largestStringInputBytes({ path: "foo.ts", content: "hello" })).toBeNull();
    expect(largestStringInputBytes({})).toBeNull();
    expect(largestStringInputBytes(null)).toBeNull();
    expect(largestStringInputBytes(undefined)).toBeNull();
  });

  it("returns the largest string field when at or above 1 KB", () => {
    const small = "x".repeat(100);
    const big = "y".repeat(2048);
    expect(largestStringInputBytes({ path: small, content: big })).toBe(2048);
  });

  it("ignores non-string fields when picking the largest", () => {
    const big = "z".repeat(1500);
    expect(largestStringInputBytes({ count: 9999, content: big, opts: { x: 1 } })).toBe(1500);
  });

  it("handles a string-typed args directly", () => {
    expect(largestStringInputBytes("a".repeat(2000))).toBe(2000);
    expect(largestStringInputBytes("short")).toBeNull();
  });

  it("returns the LARGEST when multiple fields exceed the threshold", () => {
    expect(
      largestStringInputBytes({
        a: "p".repeat(1100),
        b: "q".repeat(5000),
        c: "r".repeat(2000),
      }),
    ).toBe(5000);
  });
});
