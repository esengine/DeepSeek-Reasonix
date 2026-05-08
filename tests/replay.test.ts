import { describe, expect, it } from "vitest";
import type { TranscriptRecord } from "../src/transcript/log.js";
import {
  computeCumulativeStats,
  computeReplayStats,
  groupRecordsByTurn,
} from "../src/transcript/replay.js";

const mkAssistant = (
  turn: number,
  hit: number,
  miss: number,
  completion: number,
  cost: number,
  prefixHash = "stable123",
): TranscriptRecord => ({
  ts: "2026-04-21T00:00:00Z",
  turn,
  role: "assistant_final",
  content: `reply ${turn}`,
  model: "deepseek-chat",
  prefixHash,
  usage: {
    prompt_tokens: hit + miss,
    completion_tokens: completion,
    total_tokens: hit + miss + completion,
    prompt_cache_hit_tokens: hit,
    prompt_cache_miss_tokens: miss,
  },
  cost,
});

describe("computeReplayStats", () => {
  it("aggregates cache-hit and cost across assistant_final records", () => {
    const recs: TranscriptRecord[] = [
      { ts: "t", turn: 1, role: "user", content: "q1" },
      mkAssistant(1, 900, 100, 50, 0.0001),
      { ts: "t", turn: 1, role: "tool", content: "{}", tool: "foo", args: "{}" },
      { ts: "t", turn: 2, role: "user", content: "q2" },
      mkAssistant(2, 950, 50, 30, 0.00008),
    ];
    const stats = computeReplayStats(recs);
    expect(stats.turns).toBe(2);
    expect(stats.userTurns).toBe(2);
    expect(stats.toolCalls).toBe(1);
    // cache: hit 1850 / (1850+150) = 92.5%
    expect(stats.cacheHitRatio).toBeCloseTo(0.925, 4);
    expect(stats.totalCostUsd).toBeCloseTo(0.00018, 6);
    expect(stats.prefixHashes).toEqual(["stable123"]);
    expect(stats.models).toEqual(["deepseek-chat"]);
  });

  it("detects prefix churn when multiple hashes appear (baseline-style transcript)", () => {
    const recs: TranscriptRecord[] = [
      mkAssistant(1, 100, 900, 50, 0.0003, "hashA"),
      mkAssistant(2, 100, 900, 50, 0.0003, "hashB"),
      mkAssistant(3, 100, 900, 50, 0.0003, "hashC"),
    ];
    const stats = computeReplayStats(recs);
    expect(stats.prefixHashes).toHaveLength(3);
    expect(stats.cacheHitRatio).toBeCloseTo(0.1, 2);
  });

  it("tolerates old transcripts without usage — produces zero-cost stats gracefully", () => {
    const recs: TranscriptRecord[] = [
      { ts: "t", turn: 1, role: "user", content: "q" },
      { ts: "t", turn: 1, role: "assistant_final", content: "a" },
    ];
    const stats = computeReplayStats(recs);
    expect(stats.turns).toBe(0); // no usage → no perTurn entries → turns count is 0
    expect(stats.userTurns).toBe(1);
    expect(stats.totalCostUsd).toBe(0);
    expect(stats.cacheHitRatio).toBe(0);
  });
});

describe("groupRecordsByTurn (replay nav)", () => {
  it("groups records by turn and preserves in-turn order", () => {
    const recs: TranscriptRecord[] = [
      { ts: "t", turn: 1, role: "user", content: "q1" },
      mkAssistant(1, 100, 0, 10, 0),
      { ts: "t", turn: 1, role: "tool", content: "r", tool: "foo", args: "{}" },
      { ts: "t", turn: 2, role: "user", content: "q2" },
      mkAssistant(2, 100, 0, 10, 0),
    ];
    const pages = groupRecordsByTurn(recs);
    expect(pages).toHaveLength(2);
    expect(pages[0]!.turn).toBe(1);
    expect(pages[0]!.records).toHaveLength(3);
    expect(pages[0]!.records[0]!.role).toBe("user");
    expect(pages[0]!.records[2]!.role).toBe("tool");
    expect(pages[1]!.turn).toBe(2);
    expect(pages[1]!.records).toHaveLength(2);
  });

  it("returns pages sorted by turn even if records appear out of order", () => {
    const recs: TranscriptRecord[] = [
      { ts: "t", turn: 3, role: "user", content: "c" },
      { ts: "t", turn: 1, role: "user", content: "a" },
      { ts: "t", turn: 2, role: "user", content: "b" },
    ];
    const pages = groupRecordsByTurn(recs);
    expect(pages.map((p) => p.turn)).toEqual([1, 2, 3]);
  });

  it("handles an empty record list", () => {
    expect(groupRecordsByTurn([])).toEqual([]);
  });
});

describe("computeCumulativeStats (replay nav)", () => {
  it("grows monotonically as the cursor advances", () => {
    const pages = groupRecordsByTurn([
      mkAssistant(1, 100, 900, 10, 0.001),
      mkAssistant(2, 200, 800, 10, 0.002),
      mkAssistant(3, 300, 700, 10, 0.003),
    ]);
    const s0 = computeCumulativeStats(pages, 0);
    const s1 = computeCumulativeStats(pages, 1);
    const s2 = computeCumulativeStats(pages, 2);
    expect(s0.turns).toBe(1);
    expect(s1.turns).toBe(2);
    expect(s2.turns).toBe(3);
    expect(s0.totalCostUsd).toBeLessThan(s1.totalCostUsd);
    expect(s1.totalCostUsd).toBeLessThan(s2.totalCostUsd);
  });

  it("returns empty stats for upToIdx < 0", () => {
    const pages = groupRecordsByTurn([mkAssistant(1, 100, 900, 10, 0.001)]);
    const s = computeCumulativeStats(pages, -1);
    expect(s.turns).toBe(0);
    expect(s.totalCostUsd).toBe(0);
    expect(s.prefixHashes).toEqual([]);
  });

  it("clamps gracefully when upToIdx exceeds page count", () => {
    const pages = groupRecordsByTurn([mkAssistant(1, 100, 900, 10, 0.001)]);
    const s = computeCumulativeStats(pages, 99);
    expect(s.turns).toBe(1); // only the one real page contributes
  });
});
