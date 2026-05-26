import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync("dashboard/src/styles.css", "utf8");

function cssRule(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = new RegExp(`${escaped}\\s*\\{(?<body>[^}]*)\\}`).exec(css);
  return match?.groups?.body ?? "";
}

describe("dashboard sidebar new chat button layout", () => {
  it("keeps localized labels on one line in the compact desktop sidebar", () => {
    expect(cssRule(".side-head .new-btn")).toContain("flex-wrap: nowrap");
    expect(cssRule(".side-head .new-btn")).toContain("min-width: 0");
    expect(cssRule(".side-head .new-btn svg")).toContain("flex: 0 0 auto");
    expect(cssRule(".side-head .new-btn > span:not(.shortcut)")).toContain("white-space: nowrap");
    expect(cssRule(".side-head .new-btn > span:not(.shortcut)")).toContain(
      "text-overflow: ellipsis",
    );
    expect(cssRule(".side-head .new-btn .shortcut")).toContain("flex: 0 0 auto");
  });
});
