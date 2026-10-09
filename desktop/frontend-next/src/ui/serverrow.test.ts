import { describe, expect, it } from "vitest";
import { EN } from "../i18n/en";

// MCP_STATE is a lookup table rather than a t() call, so the catalogue test
// beside this one never sees it: that test reads the arguments of t()/tx()/
// plural(), and a map value is not one. The state a row draws is chosen at
// runtime, so the only way to know the English window has words for it — and
// that a state the host can answer with was not left out of the table — is to
// read the table itself.
const SOURCES = import.meta.glob("./ServerRow.tsx", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const HEALTH_SOURCES = import.meta.glob("../../../../internal/session/control/mcp_health.go", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const HEALTH_SOURCE = Object.values(HEALTH_SOURCES)[0] ?? "";
const HOST_STATES = [...HEALTH_SOURCE.matchAll(/^\s*MCPHealth\w+\s*=\s*"(\w+)"/gm)].map((m) => m[1]);

function stateBlock(): string {
  const src = Object.values(SOURCES)[0] ?? "";
  const start = src.indexOf("const MCP_STATE");
  expect(start, "ServerRow.tsx no longer declares MCP_STATE").toBeGreaterThan(-1);
  return src.slice(start, src.indexOf("};", start));
}

describe("MCP state labels", () => {
  it("has a label for every state the host can report", () => {
    expect(HOST_STATES.length, "No host MCP health declarations were read").toBeGreaterThan(0);
    const block = stateBlock();
    const labelled = [...block.matchAll(/^\s{2}(\w+):/gm)].map((m) => m[1]);
    for (const state of HOST_STATES) {
      expect(labelled, `MCP_STATE is missing ${state}`).toContain(state);
    }
  });

  it("carries every label in the English catalogue", () => {
    const labels = [...stateBlock().matchAll(/: "([^"]+)"/g)].map((m) => m[1]);
    expect(labels.length).toBeGreaterThan(0);
    for (const label of labels) {
      expect(EN[label], `${label} is not in the catalogue`).toBeTruthy();
    }
  });
});
