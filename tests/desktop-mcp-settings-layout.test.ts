import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const cssPath = fileURLToPath(new URL("../desktop/src/styles.css", import.meta.url));
const css = readFileSync(cssPath, "utf8");

function cssRule(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = new RegExp(`${escaped}\\s*\\{(?<body>[^}]*)\\}`).exec(css);
  return match?.groups?.body ?? "";
}

describe("desktop MCP settings layout", () => {
  it("keeps long MCP specs inside the card instead of widening the modal", () => {
    expect(cssRule(".scard .mcp-spec-body")).toContain("min-width: 0");
    expect(cssRule(".scard .mcp-spec-body")).toContain("flex: 1 1 auto");
    expect(cssRule(".scard .mcp-spec-summary")).toContain("overflow-wrap: anywhere");
    expect(cssRule(".scard .mcp-spec-summary")).toContain("word-break: break-word");
    expect(cssRule(".scard .mcp-remove")).toContain("flex: 0 0 auto");
  });
});
