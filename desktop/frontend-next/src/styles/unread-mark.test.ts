import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob(["./studio.css", "../design/rows.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = Object.values(SHEETS).join("\n");
const rule = (selector: string) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [...css.matchAll(new RegExp(`(?:^|\\n)${esc}\\s*\\{([^}]*)\\}`, "g"))].map((m) => m[1]).join(";");
};

describe("the unread mark in the rail", () => {
  it("draws the folded count unlike the feedback badge", () => {
    const pill = rule(".ds-proj .wsacts > .unread-count");
    expect(pill).toMatch(/border:\s*1px solid/);
    expect(pill).not.toMatch(/background:\s*var\(--accent\)/);
    expect(pill).not.toMatch(/grid-area/);
  });
});
