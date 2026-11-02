import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob(["./app.css", "./studio.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const layer = (sheet: string, selector: string) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\>]/g, "\\$&");
  const body = SHEETS[sheet].match(new RegExp(`(?:^|\\n)\\s*${esc}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
  return Number(body.match(/z-index:\s*(\d+)/)?.[1]);
};

describe("composer layers", () => {
  it("keeps the controls row, and the menus it opens, above the plan bar that floats over it", () => {
    expect(layer("./app.css", ".compose .row > *")).toBeGreaterThan(layer("./studio.css", ".studio-todo"));
  });
});
