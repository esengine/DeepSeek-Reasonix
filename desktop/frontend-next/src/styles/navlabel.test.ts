import { describe, expect, it } from "vitest";

const STUDIO = Object.values(
  import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>,
)[0];

const rule = (selector: string) => {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\>]/g, "\\$&");
  return STUDIO.match(new RegExp(String.raw`(?:^|\n)\s*${escaped}\s*\{([^}]*)\}`))?.[1] ?? "";
};

describe("the sidebar's destination labels", () => {
  it("sit lower in their box so a CJK label centres on its icon", () => {
    expect(rule(".studio-quicknav b")).toMatch(/padding-top:\s*2px/);
    expect(rule(".studio-search > .studio-label")).toMatch(/padding-top:\s*3px/);
  });
});
