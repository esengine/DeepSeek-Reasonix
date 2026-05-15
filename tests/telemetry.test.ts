import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import {
  DEEPSEEK_PRICING,
  SessionStats,
  cacheSavingsUsd,
  costUsd,
  inputCostUsd,
  outputCostUsd,
} from "../src/telemetry/stats.js";

// Derive expected figures from the pricing table so the tests don't
// re-bake stale constants every time DeepSeek updates the price sheet.
// The `costUsd` formula under test is:
//   (hitT * hit + missT * miss + outT * out) / 1e6
const CHAT = DEEPSEEK_PRICING["deepseek-chat"]!;

describe("Usage.cacheHitRatio", () => {
  it("computes hit ratio", () => {
    const u = new Usage(0, 0, 0, 80, 20);
    expect(u.cacheHitRatio).toBe(0.8);
  });
  it("is zero on empty", () => {
    expect(new Usage().cacheHitRatio).toBe(0);
  });

  it("falls back cache miss tokens from prompt minus cache hit when the API omits miss", () => {
    const u = Usage.fromApi({
      prompt_tokens: 1000,
      completion_tokens: 100,
      total_tokens: 1100,
      prompt_cache_hit_tokens: 800,
    });
    expect(u.promptCacheHitTokens).toBe(800);
    expect(u.promptCacheMissTokens).toBe(200);
  });
});

describe("costUsd", () => {
  it("matches DeepSeek's published V4 USD pricing sheet", () => {
    expect(DEEPSEEK_PRICING["deepseek-v4-flash"]).toEqual({
      inputCacheHit: 0.0028,
      inputCacheMiss: 0.14,
      output: 0.28,
    });
    expect(DEEPSEEK_PRICING["deepseek-v4-pro"]).toEqual({
      inputCacheHit: 0.003625,
      inputCacheMiss: 0.435,
      output: 0.87,
    });
  });

  it("applies DeepSeek pricing tiers", () => {
    const u = new Usage(1000, 100, 0, 800, 200);
    const c = costUsd("deepseek-chat", u);
    expect(c).toBeCloseTo(
      (800 * CHAT.inputCacheHit + 200 * CHAT.inputCacheMiss + 100 * CHAT.output) / 1_000_000,
      10,
    );
  });

  it("returns 0 for unknown model", () => {
    expect(costUsd("unknown-model", new Usage(1000, 100))).toBe(0);
  });
});

describe("SessionStats", () => {
  it("aggregates savings vs Claude", () => {
    const stats = new SessionStats();
    stats.record(1, "deepseek-chat", new Usage(1000, 100, 1100, 800, 200));
    const s = stats.summary();
    expect(s.turns).toBe(1);
    expect(s.cacheHitRatio).toBe(0.8);
    expect(s.savingsVsClaudePct).toBeGreaterThan(90);
  });

  it("accumulates across turns", () => {
    const stats = new SessionStats();
    stats.record(1, "deepseek-chat", new Usage(100, 10, 110, 80, 20));
    stats.record(2, "deepseek-chat", new Usage(200, 20, 220, 160, 40));
    expect(stats.turns.length).toBe(2);
    expect(stats.aggregateCacheHitRatio).toBeCloseTo(240 / 300);
  });

  it("summary.lastPromptTokens tracks the most recent turn only", () => {
    const stats = new SessionStats();
    expect(stats.summary().lastPromptTokens).toBe(0);
    stats.record(1, "deepseek-chat", new Usage(5_000, 100, 5_100, 4_000, 1_000));
    expect(stats.summary().lastPromptTokens).toBe(5_000);
    stats.record(2, "deepseek-chat", new Usage(42_000, 200, 42_200, 40_000, 2_000));
    expect(stats.summary().lastPromptTokens).toBe(42_000);
  });

  it("summary splits input + output costs — the new panel breakdown", () => {
    const stats = new SessionStats();
    stats.record(1, "deepseek-chat", new Usage(1000, 100, 1100, 800, 200));
    const s = stats.summary();
    // `summary()` rounds USD figures to 6 decimals, so we match at 6 —
    // the raw formula at higher precision is exercised by the
    // `inputCostUsd` / `outputCostUsd` tests below.
    expect(s.totalInputCostUsd).toBeCloseTo(
      (800 * CHAT.inputCacheHit + 200 * CHAT.inputCacheMiss) / 1_000_000,
      6,
    );
    expect(s.totalOutputCostUsd).toBeCloseTo((100 * CHAT.output) / 1_000_000, 6);
    // Sum of input+output equals total (within rounding).
    expect(s.totalInputCostUsd + s.totalOutputCostUsd).toBeCloseTo(s.totalCostUsd, 6);
  });

  it("reset clears live and carryover totals for /new", () => {
    const stats = new SessionStats();
    stats.seedCarryover({
      totalCostUsd: 0.05,
      turnCount: 3,
      cacheHitTokens: 1000,
      cacheMissTokens: 100,
      lastPromptTokens: 1100,
    });
    stats.record(4, "deepseek-chat", new Usage(1000, 100, 1100, 800, 200));
    stats.reset();
    expect(stats.turns).toHaveLength(0);
    expect(stats.totalCost).toBe(0);
    expect(stats.summary()).toMatchObject({
      turns: 0,
      totalCostUsd: 0,
      cacheHitRatio: 0,
      lastPromptTokens: 0,
      lastTurnCostUsd: 0,
    });
  });
});

describe("inputCostUsd / outputCostUsd", () => {
  it("input cost covers cache-hit + cache-miss but NOT completion", () => {
    const u = new Usage(1000, 100, 1100, 800, 200);
    const i = inputCostUsd("deepseek-chat", u);
    expect(i).toBeCloseTo((800 * CHAT.inputCacheHit + 200 * CHAT.inputCacheMiss) / 1_000_000, 10);
  });

  it("output cost covers completion only", () => {
    const u = new Usage(1000, 100, 1100, 800, 200);
    const o = outputCostUsd("deepseek-chat", u);
    expect(o).toBeCloseTo((100 * CHAT.output) / 1_000_000, 10);
  });

  it("chat and reasoner are unified at the same price", () => {
    // 2026-04 V4 launch: `deepseek-chat` and `deepseek-reasoner` are
    // compat aliases for v4-flash's non-thinking and thinking modes
    // respectively, so billing is identical. If this diverges, either
    // DeepSeek split them again (update the constants) or one alias
    // got out of sync during an update — catch before shipping.
    const chat = DEEPSEEK_PRICING["deepseek-chat"]!;
    const reasoner = DEEPSEEK_PRICING["deepseek-reasoner"]!;
    const flash = DEEPSEEK_PRICING["deepseek-v4-flash"]!;
    expect(reasoner).toEqual(chat);
    expect(chat).toEqual(flash);
  });

  it("v4-pro pricing is present and strictly above v4-flash", () => {
    const flash = DEEPSEEK_PRICING["deepseek-v4-flash"]!;
    const pro = DEEPSEEK_PRICING["deepseek-v4-pro"]!;
    expect(pro.inputCacheHit).toBeGreaterThan(flash.inputCacheHit);
    expect(pro.inputCacheMiss).toBeGreaterThan(flash.inputCacheMiss);
    expect(pro.output).toBeGreaterThan(flash.output);
  });

  it("v4-pro cost is computed with its own tier, not flash's", () => {
    // Sanity: passing the pro model to costUsd doesn't silently fall
    // back to flash rates, otherwise billing on pro would under-count.
    const u = new Usage(0, 100, 0, 0, 1000);
    const flashCost = costUsd("deepseek-v4-flash", u);
    const proCost = costUsd("deepseek-v4-pro", u);
    expect(proCost).toBeGreaterThan(flashCost * 3); // current pro promo is ~3.1x flash
  });

  it("both return 0 for an unknown model", () => {
    const u = new Usage(1000, 100, 1100, 800, 200);
    expect(inputCostUsd("unknown", u)).toBe(0);
    expect(outputCostUsd("unknown", u)).toBe(0);
  });
});

describe("cacheSavingsUsd", () => {
  it("returns hit-vs-miss USD diff for the given model + hit token count", () => {
    const hit = 1000;
    const expected = (hit * (CHAT.inputCacheMiss - CHAT.inputCacheHit)) / 1_000_000;
    expect(cacheSavingsUsd("deepseek-chat", hit)).toBeCloseTo(expected, 12);
  });

  it("returns 0 when hit tokens are zero", () => {
    expect(cacheSavingsUsd("deepseek-chat", 0)).toBe(0);
  });

  it("returns 0 for negative input (defensive — never bills negative)", () => {
    expect(cacheSavingsUsd("deepseek-chat", -100)).toBe(0);
  });

  it("returns 0 for an unknown model", () => {
    expect(cacheSavingsUsd("never-shipped-model", 1000)).toBe(0);
  });

  it("v4-pro savings per hit token are larger than v4-flash (bigger miss/hit gap)", () => {
    // Pro's miss-to-hit gap dwarfs Flash's, so each cached pro token
    // saves more in absolute terms — useful sanity check that we picked
    // the right side of the subtraction.
    const flashSave = cacheSavingsUsd("deepseek-v4-flash", 1000);
    const proSave = cacheSavingsUsd("deepseek-v4-pro", 1000);
    expect(proSave).toBeGreaterThan(flashSave);
  });
});

describe("SessionStats — issue #333 resume cost carryover", () => {
  it("totalCost includes seeded carryover plus live turns", () => {
    const s = new SessionStats();
    s.seedCarryover({ totalCostUsd: 0.05, turnCount: 3 });
    s.record(4, "deepseek-chat", new Usage(1000, 100, 0, 800, 200));
    expect(s.totalCost).toBeGreaterThan(0.05);
    expect(s.summary().totalCostUsd).toBeGreaterThan(0.05);
    expect(s.summary().turns).toBe(4);
  });

  it("seedCarryover ignores undefined / zero / negative inputs", () => {
    const s = new SessionStats();
    s.seedCarryover({ totalCostUsd: 0, turnCount: 0 });
    s.seedCarryover({ totalCostUsd: -1 });
    expect(s.totalCost).toBe(0);
    expect(s.summary().turns).toBe(0);
  });

  it("zero carryover keeps totalCost equal to live-turn sum (regression: no double-count for fresh sessions)", () => {
    const s = new SessionStats();
    s.record(1, "deepseek-chat", new Usage(1000, 100, 0, 800, 200));
    const live = s.totalCost;
    expect(live).toBeGreaterThan(0);
    s.seedCarryover({});
    expect(s.totalCost).toBe(live);
  });
});

describe("SessionStats — issue #364 resume cache + context carryover", () => {
  it("aggregateCacheHitRatio includes carryover so /status isn't 0% on a fresh resume", () => {
    const s = new SessionStats();
    s.seedCarryover({ cacheHitTokens: 366976, cacheMissTokens: 109 });
    // No live turns yet — ratio must come from the carryover alone.
    expect(s.aggregateCacheHitRatio).toBeCloseTo(366976 / (366976 + 109), 4);
    expect(s.summary().cacheHitRatio).toBeCloseTo(366976 / (366976 + 109), 4);
  });

  it("aggregateCacheHitRatio sums carryover + live turns", () => {
    const s = new SessionStats();
    s.seedCarryover({ cacheHitTokens: 1000, cacheMissTokens: 0 });
    s.record(1, "deepseek-chat", new Usage(2000, 100, 0, 0, 2000));
    // 1000 hit (carryover) + 0 hit (live) over 1000 + 2000 = 1/3.
    expect(s.aggregateCacheHitRatio).toBeCloseTo(1000 / 3000, 4);
  });

  it("summary.lastPromptTokens falls back to carryover before any live turn", () => {
    const s = new SessionStats();
    s.seedCarryover({ lastPromptTokens: 367085 });
    expect(s.summary().lastPromptTokens).toBe(367085);
  });

  it("live turn overrides carryover lastPromptTokens", () => {
    const s = new SessionStats();
    s.seedCarryover({ lastPromptTokens: 100 });
    s.record(1, "deepseek-chat", new Usage(500, 50, 0, 400, 100));
    expect(s.summary().lastPromptTokens).toBe(500);
  });

  it("seedCarryover ignores zero / negative cache + context fields", () => {
    const s = new SessionStats();
    s.seedCarryover({ cacheHitTokens: 0, cacheMissTokens: -5, lastPromptTokens: 0 });
    expect(s.aggregateCacheHitRatio).toBe(0);
    expect(s.summary().lastPromptTokens).toBe(0);
  });
});
