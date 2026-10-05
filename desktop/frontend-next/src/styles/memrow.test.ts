import { describe, expect, it } from "vitest";

const CSS = Object.values(
  import.meta.glob("./app.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>,
)[0];

function declarations(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const m = CSS.match(new RegExp(String.raw`(?:^|\n)\s*${escaped}\s*\{([^}]*)\}`));
  return m?.[1] ?? "";
}

describe("a memory row stays inside its container", () => {
  const name = declarations(".memrow .nm");

  it("lets the title shrink and wrap instead of pushing the row wider", () => {
    expect(name, "no .memrow .nm rule found").not.toBe("");
    expect(name).not.toMatch(/flex:\s*none/);
    expect(name).toMatch(/overflow-wrap:\s*anywhere/);
    expect(name).toMatch(/max-width:/);
  });

  it("keeps the trailing controls whole", () => {
    expect(declarations(".memrow .act")).toMatch(/white-space:\s*nowrap/);
  });
});
