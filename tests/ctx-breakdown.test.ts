import { describe, expect, it } from "vitest";
import { computeCtxBreakdown } from "../src/cli/ui/ctx-breakdown.js";
import type { CacheFirstLoop } from "../src/loop.js";
import type { ChatMessage } from "../src/types.js";

function fakeLoop(messages: ChatMessage[]): CacheFirstLoop {
  return {
    model: "deepseek-v4-flash",
    prefix: { system: "system", toolSpecs: [] },
    log: { toMessages: () => messages },
  } as unknown as CacheFirstLoop;
}

describe("computeCtxBreakdown", () => {
  it("bounds token counting for pathological large tool results", () => {
    const loop = fakeLoop([
      { role: "user", content: "read log" },
      { role: "tool", name: "read_file", tool_call_id: "t1", content: "A".repeat(100_000) },
    ]);

    const t0 = performance.now();
    const breakdown = computeCtxBreakdown(loop);
    const t1 = performance.now();

    expect(breakdown.logTokens).toBeGreaterThan(0);
    expect(breakdown.topTools[0]?.name).toBe("read_file");
    expect(t1 - t0).toBeLessThan(1000);
  });
});
