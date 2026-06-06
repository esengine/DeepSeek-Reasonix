import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import { ContextManager, estimateFoldEconomics } from "../src/context-manager.js";

function manager(): ContextManager {
  return new ContextManager({
    client: {} as never,
    log: {} as never,
    stats: {} as never,
    sessionName: null,
    getAbortSignal: () => new AbortController().signal,
    getCurrentTurn: () => 1,
    getSystemPrompt: () => "system",
  });
}

describe("ContextManager fold economics", () => {
  it("does not fold in the normal band when cache carry cost is cheaper than fold tax", () => {
    const usage = new Usage(760_000, 100, 760_100, 752_000, 8_000);
    const decision = manager().decideAfterUsage(usage, "deepseek-v4-flash", false);

    expect(decision.kind).toBe("none");
    expect(decision.economics?.worthwhile).toBe(false);
  });

  it("folds in the normal band when high miss tokens make carrying context expensive", () => {
    const usage = new Usage(760_000, 100, 760_100, 0, 760_000);
    const decision = manager().decideAfterUsage(usage, "deepseek-v4-flash", false);

    expect(decision.kind).toBe("fold");
    expect(decision.economics?.worthwhile).toBe(true);
  });

  it("still folds aggressively for headroom even if cache economics are cheap", () => {
    const usage = new Usage(790_000, 100, 790_100, 782_000, 8_000);
    const decision = manager().decideAfterUsage(usage, "deepseek-v4-flash", false);

    expect(decision.kind).toBe("fold");
    expect(decision.aggressive).toBe(true);
  });

  it("estimates fold cost over a short multi-turn horizon", () => {
    const usage = new Usage(760_000, 100, 760_100, 0, 760_000);
    const economics = estimateFoldEconomics(usage, "deepseek-v4-flash", 200_000);

    expect(economics.horizonTurns).toBeGreaterThan(1);
    expect(economics.carryInputUsd).toBeGreaterThan(economics.foldInputUsd);
    expect(economics.savingsUsd).toBeGreaterThan(0);
  });
});
