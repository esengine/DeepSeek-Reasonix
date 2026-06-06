import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import { AppendOnlyLog, ImmutablePrefix } from "../src/memory/runtime.js";
import { SessionStats } from "../src/telemetry/stats.js";
import { ToolRegistry } from "../src/tools.js";
import type { ToolSpec } from "../src/types.js";

function tool(name: string): ToolSpec {
  return {
    type: "function",
    function: {
      name,
      description: `${name} tool`,
      parameters: { type: "object", properties: {} },
    },
  };
}

describe("cache-shape diagnostics", () => {
  it("sorts ToolRegistry specs by tool name for stable prefix bytes", () => {
    const reg = new ToolRegistry();
    reg.register({ name: "zeta", fn: async () => "z" });
    reg.register({ name: "alpha", fn: async () => "a" });

    expect(reg.specs().map((s) => s.function.name)).toEqual(["alpha", "zeta"]);
  });

  it("normalizes ImmutablePrefix tool order before hashing", () => {
    const a = new ImmutablePrefix({ system: "s", toolSpecs: [tool("read"), tool("write")] });
    const b = new ImmutablePrefix({ system: "s", toolSpecs: [tool("write"), tool("read")] });

    expect(b.fingerprint).toBe(a.fingerprint);
    expect(b.componentHashes.tools).toBe(a.componentHashes.tools);
  });

  it("keeps hot-added tools sorted inside the prefix", () => {
    const prefix = new ImmutablePrefix({ system: "s", toolSpecs: [tool("write")] });
    expect(prefix.addTool(tool("read"))).toBe(true);

    expect(prefix.toolSpecs.map((s) => s.function.name)).toEqual(["read", "write"]);
  });

  it("tracks append-only-breaking rewrites separately from normal appends", () => {
    const log = new AppendOnlyLog();
    expect(log.rewriteVersion).toBe(0);

    log.append({ role: "user", content: "hello" });
    expect(log.rewriteVersion).toBe(0);

    log.compactInPlace([{ role: "assistant", content: "summary" }]);
    expect(log.rewriteVersion).toBe(1);
  });

  it("surfaces miss tokens and prefix churn in session summaries", () => {
    const stats = new SessionStats();
    stats.record(1, "deepseek-v4-flash", new Usage(1000, 50, 1050, 900, 100), {
      prefixHash: "p1",
      prefixChanged: true,
      prefixChangeReasons: ["tools"],
      systemHash: "s1",
      toolsHash: "t1",
      fewShotsHash: "f1",
      logRewriteVersion: 2,
      toolSchemaTokens: 345,
      promptCacheHitTokens: 900,
      promptCacheMissTokens: 100,
    });

    const summary = stats.summary();
    expect(summary.totalCacheHitTokens).toBe(900);
    expect(summary.totalCacheMissTokens).toBe(100);
    expect(summary.lastCacheMissTokens).toBe(100);
    expect(summary.lastToolSchemaTokens).toBe(345);
    expect(summary.lastPrefixChanged).toBe(true);
    expect(summary.lastPrefixChangeReasons).toEqual(["tools"]);
  });
});
