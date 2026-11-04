// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { pageHome, SECTION_NAME } from "./prefsnav";

describe("a folded settings page keeps its old name as a sub-section of the page that holds it", () => {
  it("sends every folded page to a page the navigation lists", () => {
    for (const from of ["providers", "remote", "memory", "usage", "versions", "persona", "peers", "hooks"] as const) {
      const home = pageHome(from);
      expect(home).not.toBe(from);
      expect(SECTION_NAME[home]).toBeTruthy();
      expect(SECTION_NAME[from]).toBeTruthy();
    }
  });
});
