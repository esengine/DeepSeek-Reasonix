import { describe, expect, it } from "vitest";
import { type AnsiCode, StylePool } from "../../src/renderer/pools/style-pool.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };
const BOLD: AnsiCode = { apply: "\x1b[1m", revert: "\x1b[22m" };
const UNDERLINE: AnsiCode = { apply: "\x1b[4m", revert: "\x1b[24m" };

describe("StylePool", () => {
  it("none is id 0 and matches empty stack", () => {
    const p = new StylePool();
    expect(p.none).toBe(0);
    expect(p.intern([])).toBe(0);
  });

  it("intern is idempotent regardless of insertion order", () => {
    const p = new StylePool();
    const a = p.intern([RED, BOLD]);
    const b = p.intern([BOLD, RED]);
    expect(a).toBe(b);
  });

  it("distinct stacks get distinct ids", () => {
    const p = new StylePool();
    const a = p.intern([RED]);
    const b = p.intern([BOLD]);
    const c = p.intern([RED, BOLD]);
    expect(new Set([a, b, c]).size).toBe(3);
  });

  it("transition from none to red emits red apply", () => {
    const p = new StylePool();
    const red = p.intern([RED]);
    expect(p.transition(p.none, red)).toBe("\x1b[31m");
  });

  it("transition from red to none emits red revert", () => {
    const p = new StylePool();
    const red = p.intern([RED]);
    expect(p.transition(red, p.none)).toBe("\x1b[39m");
  });

  it("transition between styles emits revert(removed) + apply(added)", () => {
    const p = new StylePool();
    const a = p.intern([RED, BOLD]);
    const b = p.intern([BOLD, UNDERLINE]);
    const out = p.transition(a, b);
    // RED dropped → revert it; UNDERLINE added → apply it. BOLD persists.
    expect(out).toContain("\x1b[39m"); // RED revert
    expect(out).toContain("\x1b[4m"); // UNDERLINE apply
    expect(out).not.toContain("\x1b[1m"); // BOLD already on
    expect(out).not.toContain("\x1b[22m"); // BOLD not reverted
  });

  it("transition fromId === toId returns empty string", () => {
    const p = new StylePool();
    const id = p.intern([RED]);
    expect(p.transition(id, id)).toBe("");
  });

  it("transition cache returns identical strings on repeated calls", () => {
    const p = new StylePool();
    const a = p.intern([RED]);
    const b = p.intern([BOLD]);
    const first = p.transition(a, b);
    const second = p.transition(a, b);
    expect(first).toBe(second);
  });
});
