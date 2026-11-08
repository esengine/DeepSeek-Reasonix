import { describe, expect, it } from "vitest";
import css from "./rows.css?raw";

const rule = (selector: string) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [...css.matchAll(new RegExp(`(?:^|\\n)${esc}\\s*\\{([^}]*)\\}`, "g"))].map((m) => m[1]).join(";");
};

describe("session and project rows follow the board", () => {
  it("is a 30px row with a 14px lead column and 8px gaps", () => {
    const row = rule(".ds-row");
    expect(row).toMatch(/height:\s*30px/);
    expect(row).toMatch(/gap:\s*8px/);
    expect(row).toMatch(/padding:\s*0 8px 0 14px/);
    expect(row).toMatch(/border-radius:\s*var\(--r-sm\)/);
    expect(rule(".ds-lead")).toMatch(/width:\s*14px/);
  });

  it("marks state with a 7px dot: blue running, orange waiting, text colour unread", () => {
    expect(rule(".ds-dot")).toMatch(/width:\s*7px[^}]*height:\s*7px/);
    expect(rule('.ds-row[data-run="running"] .ds-dot')).toMatch(/var\(--net\)/);
    expect(rule('.ds-row[data-run="halt"] .ds-dot')).toMatch(/var\(--warn\)/);
    expect(rule(".ds-dot")).toMatch(/background:\s*var\(--text\)/);
  });

  it("fills a selected row with the overlay and a hovered one with sixty percent of it", () => {
    expect(rule(".ds-row[data-on]")).toMatch(/background:\s*var\(--overlay\)/);
    expect(rule(".ds-row:hover")).toMatch(/color-mix\(in srgb, var\(--overlay\) 60%, transparent\)/);
  });

  it("swaps the time for the more button on hover", () => {
    expect(rule(".ds-row:is(:hover, :focus-within) > :is(.ds-meta, .ds-pin)")).toMatch(/display:\s*none/);
    expect(rule(".ds-row:is(:hover, :focus-within) .ds-more")).toMatch(/display:\s*grid/);
  });

  it("hides the quick buttons until the row is hovered or focused, without taking them out of the tab order", () => {
    const hidden = rule(".ds-row:not(:is(:hover, :focus-within)) :is(.sessquick, .sesspin-action)");
    expect(hidden).toMatch(/opacity:\s*0/);
    expect(hidden).toMatch(/pointer-events:\s*none/);
    expect(hidden).not.toMatch(/display:\s*none/);
    expect(rule(".ds-row .sesspin-action")).toMatch(/display:\s*grid/);
    expect(rule(".ds-row .sessquick, .ds-row .sesspin-action")).not.toMatch(/display:\s*none/);
  });
});
