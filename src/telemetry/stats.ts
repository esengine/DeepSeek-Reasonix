import type { Usage } from "../client.js";

/** USD per 1M tokens; CNY sheet converted at fixed 7.2 — revisit if FX moves >±5%. */
export const DEEPSEEK_PRICING: Record<
  string,
  { inputCacheHit: number; inputCacheMiss: number; output: number }
> = {
  "deepseek-v4-flash": { inputCacheHit: 0.028, inputCacheMiss: 0.139, output: 0.278 },
  "deepseek-v4-pro": { inputCacheHit: 0.139, inputCacheMiss: 1.667, output: 3.333 },
  // Compat aliases — priced as v4-flash per the deprecation notice.
  "deepseek-chat": { inputCacheHit: 0.028, inputCacheMiss: 0.139, output: 0.278 },
  "deepseek-reasoner": { inputCacheHit: 0.028, inputCacheMiss: 0.139, output: 0.278 },
};

/** Reference Claude Sonnet 4.6 pricing (USD per 1M tokens). */
export const CLAUDE_SONNET_PRICING = { input: 3.0, output: 15.0 };

/** Prompt-side window only; completion caps live server-side and don't affect this gauge. */
export const DEEPSEEK_CONTEXT_TOKENS: Record<string, number> = {
  "deepseek-v4-flash": 1_000_000,
  "deepseek-v4-pro": 1_000_000,
  "deepseek-chat": 1_000_000,
  "deepseek-reasoner": 1_000_000,
};

/** Fallback when the caller's model id isn't in the table — safe lower bound. */
export const DEFAULT_CONTEXT_TOKENS = 131_072;

export function costUsd(model: string, usage: Usage): number {
  const p = DEEPSEEK_PRICING[model];
  if (!p) return 0;
  return (
    (usage.promptCacheHitTokens * p.inputCacheHit +
      usage.promptCacheMissTokens * p.inputCacheMiss +
      usage.completionTokens * p.output) /
    1_000_000
  );
}

/** Input-side cost only (prompt, cache hit + miss). Used for the panel breakdown. */
export function inputCostUsd(model: string, usage: Usage): number {
  const p = DEEPSEEK_PRICING[model];
  if (!p) return 0;
  return (
    (usage.promptCacheHitTokens * p.inputCacheHit +
      usage.promptCacheMissTokens * p.inputCacheMiss) /
    1_000_000
  );
}

/** Output-side cost only (completion tokens). Used for the panel breakdown. */
export function outputCostUsd(model: string, usage: Usage): number {
  const p = DEEPSEEK_PRICING[model];
  if (!p) return 0;
  return (usage.completionTokens * p.output) / 1_000_000;
}

export function cacheSavingsUsd(model: string, hitTokens: number): number {
  if (hitTokens <= 0) return 0;
  const p = DEEPSEEK_PRICING[model];
  if (!p) return 0;
  return (hitTokens * (p.inputCacheMiss - p.inputCacheHit)) / 1_000_000;
}

export function claudeEquivalentCost(usage: Usage): number {
  return (
    (usage.promptTokens * CLAUDE_SONNET_PRICING.input +
      usage.completionTokens * CLAUDE_SONNET_PRICING.output) /
    1_000_000
  );
}

export interface TurnStats {
  turn: number;
  model: string;
  usage: Usage;
  cost: number;
  cacheHitRatio: number;
}

export interface SessionSummary {
  turns: number;
  totalCostUsd: number;
  totalInputCostUsd: number;
  /** Output-side (completion) cost aggregated across the session. */
  totalOutputCostUsd: number;
  /** @deprecated Claude reference; kept for benchmarks + replay compat, no longer surfaced in the TUI. */
  claudeEquivalentUsd: number;
  /** @deprecated. Same as claudeEquivalentUsd — synthetic ratio, not a real measurement. */
  savingsVsClaudePct: number;
  cacheHitRatio: number;
  /** Floor estimate for next call — actual cost = this + user delta + new tool outputs. */
  lastPromptTokens: number;
  lastTurnCostUsd: number;
}

export class SessionStats {
  readonly turns: TurnStats[] = [];
  /** Cost from prior runs of a resumed session, restored from session meta. */
  private _carryoverCost = 0;
  /** Turn count from prior runs of a resumed session. */
  private _carryoverTurns = 0;

  /** Seed totals from a resumed session's persisted meta — only call once at construction. */
  seedCarryover(opts: { totalCostUsd?: number; turnCount?: number }): void {
    if (typeof opts.totalCostUsd === "number" && opts.totalCostUsd > 0) {
      this._carryoverCost = opts.totalCostUsd;
    }
    if (typeof opts.turnCount === "number" && opts.turnCount > 0) {
      this._carryoverTurns = opts.turnCount;
    }
  }

  record(turn: number, model: string, usage: Usage): TurnStats {
    const cost = costUsd(model, usage);
    const stats: TurnStats = {
      turn,
      model,
      usage,
      cost,
      cacheHitRatio: usage.cacheHitRatio,
    };
    this.turns.push(stats);
    return stats;
  }

  get totalCost(): number {
    return this._carryoverCost + this.turns.reduce((sum, t) => sum + t.cost, 0);
  }

  get totalClaudeEquivalent(): number {
    return this.turns.reduce((sum, t) => sum + claudeEquivalentCost(t.usage), 0);
  }

  get savingsVsClaude(): number {
    const c = this.totalClaudeEquivalent;
    return c > 0 ? 1 - this.totalCost / c : 0;
  }

  get totalInputCost(): number {
    return this.turns.reduce((sum, t) => sum + inputCostUsd(t.model, t.usage), 0);
  }

  get totalOutputCost(): number {
    return this.turns.reduce((sum, t) => sum + outputCostUsd(t.model, t.usage), 0);
  }

  get aggregateCacheHitRatio(): number {
    let hit = 0;
    let miss = 0;
    for (const t of this.turns) {
      hit += t.usage.promptCacheHitTokens;
      miss += t.usage.promptCacheMissTokens;
    }
    const denom = hit + miss;
    return denom > 0 ? hit / denom : 0;
  }

  summary(): SessionSummary {
    const last = this.turns[this.turns.length - 1];
    return {
      turns: this.turns.length + this._carryoverTurns,
      totalCostUsd: round(this.totalCost, 6),
      totalInputCostUsd: round(this.totalInputCost, 6),
      totalOutputCostUsd: round(this.totalOutputCost, 6),
      claudeEquivalentUsd: round(this.totalClaudeEquivalent, 6),
      savingsVsClaudePct: round(this.savingsVsClaude * 100, 2),
      cacheHitRatio: round(this.aggregateCacheHitRatio, 4),
      lastPromptTokens: last?.usage.promptTokens ?? 0,
      lastTurnCostUsd: round(last?.cost ?? 0, 6),
    };
  }
}

function round(n: number, digits: number): number {
  const f = 10 ** digits;
  return Math.round(n * f) / f;
}
