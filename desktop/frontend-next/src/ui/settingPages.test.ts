import { describe, expect, it } from "vitest";
import { PAGES, firstPage, hiddenAnchors, pageOf, pagesOf } from "./settingPages";

describe("setting sub-pages", () => {
  it("no section is split into sub-pages any more: long pages use sub-headings", () => {
    expect(Object.keys(PAGES)).toEqual([]);
    expect(pagesOf("tools")).toEqual([]);
    expect(firstPage("tools")).toBeUndefined();
    expect(pageOf("tools", "sandbox")).toBeUndefined();
    expect(hiddenAnchors("tools", "sandbox")).toEqual([]);
  });
});
