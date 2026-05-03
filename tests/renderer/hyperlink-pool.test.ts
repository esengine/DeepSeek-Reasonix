import { describe, expect, it } from "vitest";
import { HyperlinkPool } from "../../src/renderer/pools/hyperlink-pool.js";

describe("HyperlinkPool", () => {
  it("undefined and empty string both map to id 0 (no hyperlink)", () => {
    const p = new HyperlinkPool();
    expect(p.intern(undefined)).toBe(0);
    expect(p.intern("")).toBe(0);
    expect(p.get(0)).toBeUndefined();
  });

  it("intern is idempotent", () => {
    const p = new HyperlinkPool();
    const a = p.intern("https://example.com");
    const b = p.intern("https://example.com");
    expect(a).toBe(b);
    expect(p.get(a)).toBe("https://example.com");
  });

  it("distinct uris get distinct ids", () => {
    const p = new HyperlinkPool();
    const a = p.intern("https://a.com");
    const b = p.intern("https://b.com");
    expect(a).not.toBe(b);
    expect(p.get(a)).toBe("https://a.com");
    expect(p.get(b)).toBe("https://b.com");
  });
});
