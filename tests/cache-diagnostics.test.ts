import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import {
  appendCacheDiagnostic,
  buildCacheDiagnostic,
  inferCacheMissReason,
  prefixDiagnosticHashes,
  renderCacheMissReport,
} from "../src/telemetry/cache-diagnostics.js";
import type { ToolSpec } from "../src/types.js";

function tool(name: string, parameters: object = { type: "object" }): ToolSpec {
  return {
    type: "function",
    function: { name, description: "", parameters },
  };
}

function usage(hit: number, miss: number): Usage {
  return new Usage(hit + miss, 10, hit + miss + 10, hit, miss);
}

describe("cache diagnostics", () => {
  it("breaks the immutable prefix into stable sub-hashes", () => {
    const a = prefixDiagnosticHashes({
      system: "s",
      toolSpecs: [tool("read"), tool("write")],
      fewShots: [],
    });
    const b = prefixDiagnosticHashes({
      system: "s",
      toolSpecs: [tool("read"), tool("write")],
      fewShots: [],
    });

    expect(a).toEqual(b);
    expect(a.toolNames).toEqual(["read", "write"]);
    expect(a.prefixHash).toHaveLength(16);
  });

  it("infers local miss reasons from prefix evidence", () => {
    const previous = buildCacheDiagnostic({
      turn: 1,
      model: "deepseek-v4-flash",
      usage: usage(0, 100),
      estimatedCostUsd: 0.001,
      prefix: prefixDiagnosticHashes({ system: "s", toolSpecs: [tool("read")], fewShots: [] }),
    });
    const current = prefixDiagnosticHashes({
      system: "s",
      toolSpecs: [tool("read", { type: "object", properties: { path: { type: "string" } } })],
      fewShots: [],
    });

    expect(inferCacheMissReason(previous, current, usage(50, 50))).toMatchObject({
      reason: "tool-schema-or-order-changed",
    });
    expect(
      inferCacheMissReason(
        previous,
        prefixDiagnosticHashes({ system: "s2", toolSpecs: [tool("read")], fewShots: [] }),
        usage(50, 50),
      ),
    ).toMatchObject({ reason: "system-prompt-changed" });
  });

  it("renders the DeepSeek/inferred caveat in reports", () => {
    const entry = buildCacheDiagnostic({
      turn: 1,
      model: "deepseek-v4-flash",
      usage: usage(90, 10),
      estimatedCostUsd: 0.001,
      prefix: prefixDiagnosticHashes({ system: "s", toolSpecs: [tool("read")], fewShots: [] }),
    });
    const report = renderCacheMissReport(appendCacheDiagnostic([], entry));

    expect(report).toContain("DeepSeek does not return a cache-miss reason");
    expect(report).toContain("Reasonix infers");
    expect(report).toContain("cached 90");
  });
});
