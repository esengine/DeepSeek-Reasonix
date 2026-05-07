import { describe, expect, it } from "vitest";
import { healthBadge, slashHealthBadge } from "../src/cli/ui/mcp-health.js";

describe("healthBadge", () => {
  it("returns no-inspect-data state for elapsedMs === 0", () => {
    const badge = healthBadge(0);
    expect(badge).toEqual({
      glyph: "✗",
      label: "no inspect data",
      color: "#f87171",
    });
  });

  it("returns healthy state for fast servers (< 500ms)", () => {
    const badge = healthBadge(120);
    expect(badge).toEqual({
      glyph: "●",
      label: "healthy · 120ms",
      color: "#4ade80",
    });
  });

  it("returns healthy for exactly 499ms", () => {
    const badge = healthBadge(499);
    expect(badge.label).toBe("healthy · 499ms");
    expect(badge.glyph).toBe("●");
  });

  it("returns slow state for moderate servers (< 3000ms)", () => {
    const badge = healthBadge(1500);
    expect(badge).toEqual({
      glyph: "◌",
      label: "slow · 1500ms",
      color: "#fbbf24",
    });
  });

  it("returns slow for exactly 2999ms", () => {
    const badge = healthBadge(2999);
    expect(badge.label).toBe("slow · 2999ms");
    expect(badge.glyph).toBe("◌");
  });

  it("returns very-slow state for high-latency servers (>= 3000ms)", () => {
    const badge = healthBadge(3000);
    expect(badge).toEqual({
      glyph: "✗",
      label: "very slow · 3000ms",
      color: "#f87171",
    });
  });

  it("returns very-slow for extremely slow servers", () => {
    const badge = healthBadge(12000);
    expect(badge.label).toBe("very slow · 12000ms");
    expect(badge.glyph).toBe("✗");
  });
});

describe("slashHealthBadge", () => {
  it("returns healthy string for fast servers (< 500ms)", () => {
    expect(slashHealthBadge(120)).toBe("● healthy · 120ms");
  });

  it("returns slow string for moderate servers (< 3000ms)", () => {
    expect(slashHealthBadge(1500)).toBe("◌ slow · 1500ms");
  });

  it("returns very-slow string for high-latency servers (>= 3000ms)", () => {
    expect(slashHealthBadge(3000)).toBe("✗ very slow · 3000ms");
  });

  it("treats elapsedMs === 0 as healthy (no special-casing like healthBadge)", () => {
    // slashHealthBadge lacks the === 0 branch — 0ms falls into < 500
    expect(slashHealthBadge(0)).toBe("● healthy · 0ms");
  });
});
