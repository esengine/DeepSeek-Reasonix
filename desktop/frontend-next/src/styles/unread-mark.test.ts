import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = SHEETS["./studio.css"];
const rule = (selector: string) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [...css.matchAll(new RegExp(`(?:^|\\n)${esc}\\s*\\{([^}]*)\\}`, "g"))].map((m) => m[1]).join(";");
};

describe("the unread mark in the rail", () => {
  it("sits at the row end without moving the title", () => {
    const dot = rule(".rail .sessrow > .unread-dot");
    expect(dot).toMatch(/position:\s*absolute/);
    expect(dot).toMatch(/inset-inline-end/);
    expect(dot).not.toMatch(/margin/);
    expect(rule(".rail .sessrow[data-unread] .sesstitle")).not.toMatch(/padding-inline-start|margin/);
  });

  it("tints an unread row only in dark, and never in a way that reads as hover or selection", () => {
    expect(css.split("\n").some((l) => l.startsWith(".rail .sessrow[data-unread]:not("))).toBe(false);
    const dark = css.match(/:root\[data-theme="dark"\] \.rail \.sessrow\[data-unread\]:not\(:hover\):not\(\[aria-selected="true"\]\)\s*\{([^}]*)\}/)?.[1] ?? "";
    expect(dark).toMatch(/background:\s*color-mix\(in srgb, var\(--text\) 4%, transparent\)/);
  });

  it("keeps the dot clear of the remote row's delete button", () => {
    const more = 27 + 4;
    const end = Number(rule(".rail .sessrow-remote > .unread-dot").match(/inset-inline-end:\s*(\d+)px/)?.[1]);
    expect(end).toBeGreaterThanOrEqual(more + 4);
  });

  it("draws the folded count unlike the feedback badge", () => {
    const pill = rule(".rail .wsacts > .unread-count");
    expect(pill).toMatch(/border:\s*1px solid/);
    expect(pill).not.toMatch(/background:\s*var\(--accent\)/);
    expect(pill).not.toMatch(/grid-area/);
  });
});
