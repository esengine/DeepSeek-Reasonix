import { describe, expect, it } from "vitest";
import css from "../design/workbench.css?raw";

const rule = (selector: string): string => {
  const at = css.indexOf(selector + " {");
  return at < 0 ? "" : css.slice(at, css.indexOf("}", at));
};

describe("the changes section's head in a narrow explorer", () => {
  const head = rule(".workbench-changes > .workbench-explorer-head");

  it("wraps its buttons onto a second line instead of overflowing sideways", () => {
    expect(head).toMatch(/flex-wrap:\s*wrap/);
    expect(head).toMatch(/height:\s*auto/);
  });

  it("keeps the title and the file count whole rather than breaking them a character at a time", () => {
    const labels = rule(".workbench-changes > .workbench-explorer-head > :is(span, small, .workbench-section-title)");
    expect(labels).toMatch(/flex:\s*none/);
    expect(labels).toMatch(/white-space:\s*nowrap/);
  });
});
