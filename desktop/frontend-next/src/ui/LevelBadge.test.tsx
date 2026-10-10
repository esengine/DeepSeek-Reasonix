import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { LEVEL_GLYPHS, LevelBadge } from "./LevelBadge";

const SHEETS = import.meta.glob("../styles/*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const CSS = Object.values(SHEETS).join("\n");

const markup = (props: Parameters<typeof LevelBadge>[0]) => renderToStaticMarkup(<LevelBadge {...props} />);

describe("level badge artwork", () => {
  it("draws seven levels and a locked silhouette, each a different shape", () => {
    const shapes = [0, 1, 2, 3, 4, 5, 6].map((level) => markup({ level }).replace(/ aria-[a-z]+="[^"]*"| data-[a-z]+(="[^"]*")?| role="img"/g, ""));
    expect(new Set(shapes).size).toBe(7);
    const locked = markup({ level: 3, locked: true });
    expect(shapes).not.toContain(locked.replace(/ aria-[a-z]+="[^"]*"| data-[a-z]+(="[^"]*")?| role="img"/g, ""));
    expect(LEVEL_GLYPHS).toHaveLength(7);
  });

  it("is single-colour: every paint is currentColor or none, so the theme owns the ink", () => {
    for (const level of [0, 1, 2, 3, 4, 5, 6]) {
      for (const locked of [false, true]) {
        const svg = markup({ level, locked });
        for (const m of svg.matchAll(/\b(?:fill|stroke)="([^"]*)"/g)) expect(["currentColor", "none"], `${level}${locked}`).toContain(m[1]);
        expect(svg).not.toMatch(/style=|#[0-9a-f]{3,8}\b|rgb\(|oklch\(|<image|<script|<style|xlink:href|href=/i);
      }
    }
  });

  it("carries no id, so any number of them can share a page", () => {
    for (const level of [0, 1, 2, 3, 4, 5, 6]) expect(markup({ level })).not.toMatch(/\bid=|url\(#/);
  });

  it("is hidden from assistive technology beside its words and named when it stands alone", () => {
    expect(markup({ level: 2, decorative: true })).toContain('aria-hidden="true"');
    expect(markup({ level: 2, decorative: true })).not.toContain("aria-label");
    const alone = markup({ level: 2, label: "L2 · 幼苗" });
    expect(alone).toContain('role="img"');
    expect(alone).toContain('aria-label="L2 · 幼苗"');
  });

  it("reads a level past the table as the last glyph rather than failing", () => {
    expect(markup({ level: 9 })).toContain('data-level="9"');
    expect(markup({ level: -1 })).toContain("<svg");
  });

  it("takes its ink from a badge token that is a text tone, never a status colour", () => {
    expect(CSS).toMatch(/\.fbk\s*\{[^}]*--badge-ink:\s*var\(--muted\)/);
    expect(CSS).toMatch(/\.fbk\s*\{[^}]*--badge-lock:\s*var\(--faint\)/);
    expect(CSS).toMatch(/\.lvl-badge\s*\{[^}]*color:\s*var\(--badge-ink\)/);
    expect(CSS).toMatch(/\.lvl-badge\[data-locked\]\s*\{[^}]*color:\s*var\(--badge-lock\)/);
    for (const m of CSS.matchAll(/\.lvl-badge[^{]*\{([^}]*)\}/g)) expect(m[1]).not.toMatch(/var\(--(accent|ok|warn|err|net|deleg|brand)/);
  });

  it("falls back to the system text colour under forced colours", () => {
    expect(CSS).toMatch(/@media \(forced-colors: active\)\s*\{[^}]*\.lvl-badge[^}]*CanvasText/s);
  });
});
