import { describe, expect, it } from "vitest";

const CSS = Object.values(
  import.meta.glob("./composition-presets.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>,
)[0];

function rule(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return CSS.match(new RegExp(String.raw`(?:^|\n)${escaped}\s*\{([^}]*)\}`))?.[1] ?? "";
}

describe("the preset editor's module list", () => {
  it("leave the add-module button a gap below the list above it", () => {
    const gap = rule(".composition-modules > :is(select, .composition-module) + button");
    expect(Number(gap.match(/margin-top:\s*(\d+)px/)?.[1] ?? 0)).toBeGreaterThanOrEqual(10);
  });
});
