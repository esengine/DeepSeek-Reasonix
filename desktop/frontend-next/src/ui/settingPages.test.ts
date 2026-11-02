import { describe, expect, it } from "vitest";
import { SETTINGS } from "./prefsnav";
import { PAGES, pageOf, firstPage, hiddenAnchors } from "./settingPages";

describe("the pages of a long settings section", () => {
  const paged = Object.keys(PAGES) as (keyof typeof PAGES)[];

  it("has at least two pages wherever it has any", () => {
    for (const section of paged) expect(PAGES[section]!.length, section).toBeGreaterThanOrEqual(2);
  });

  it("files every setting of a paged section on exactly one page", () => {
    for (const section of paged) {
      const anchors = SETTINGS.filter((e) => e.section === section).map((e) => e.anchor);
      const filed = PAGES[section]!.flatMap((p) => p.anchors);
      expect([...filed].sort(), `${section}: every row on one page, no page naming a row it does not have`).toEqual([...anchors].sort());
    }
  });

  it("answers which page an anchor is on, and the first page of a section", () => {
    expect(pageOf("tools", "sandbox")).toBe("sandbox");
    expect(pageOf("tools", "approval")).toBe(firstPage("tools"));
    expect(pageOf("appearance", "graphics")).toBe("window");
    expect(pageOf("session", "preset")).toBeUndefined();
    expect(firstPage("session")).toBeUndefined();
  });

  it("hides everything a page does not own and nothing it does", () => {
    const hidden = hiddenAnchors("tools", "sandbox");
    expect(hidden).toContain("approval");
    expect(hidden).not.toContain("sandbox");
    expect(hidden).not.toContain("shell");
    expect(hiddenAnchors("session", "anything")).toEqual([]);
  });
});
