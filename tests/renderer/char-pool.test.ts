import { describe, expect, it } from "vitest";
import { CharPool } from "../../src/renderer/pools/char-pool.js";

describe("CharPool", () => {
  it("seeds id 0 with space and id 1 with empty string", () => {
    const p = new CharPool();
    expect(p.get(0)).toBe(" ");
    expect(p.get(1)).toBe("");
    expect(p.intern(" ")).toBe(0);
    expect(p.intern("")).toBe(1);
  });

  it("intern is idempotent for ASCII fast-path", () => {
    const p = new CharPool();
    const a = p.intern("a");
    const b = p.intern("a");
    expect(a).toBe(b);
    expect(p.get(a)).toBe("a");
  });

  it("intern is idempotent for non-ASCII (CJK, emoji ZWJ)", () => {
    const p = new CharPool();
    const cjk1 = p.intern("中");
    const cjk2 = p.intern("中");
    const family = p.intern("👨‍👩‍👧");
    const family2 = p.intern("👨‍👩‍👧");
    expect(cjk1).toBe(cjk2);
    expect(family).toBe(family2);
    expect(cjk1).not.toBe(family);
    expect(p.get(cjk1)).toBe("中");
    expect(p.get(family)).toBe("👨‍👩‍👧");
  });

  it("distinct strings get distinct ids", () => {
    const p = new CharPool();
    const a = p.intern("a");
    const b = p.intern("b");
    const c = p.intern("ab");
    expect(new Set([a, b, c]).size).toBe(3);
  });

  it("size grows with new entries; stays flat on hits", () => {
    const p = new CharPool();
    const initial = p.size;
    p.intern("x");
    p.intern("x");
    p.intern("y");
    expect(p.size).toBe(initial + 2);
  });

  it("get on an unknown id falls back to space", () => {
    const p = new CharPool();
    expect(p.get(9999)).toBe(" ");
  });
});
