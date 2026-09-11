export interface ContextWindowPercentages {
  raw: number;
  display: number;
}

export function contextWindowPercentages(usedTokens: number, windowTokens: number): ContextWindowPercentages {
  if (!Number.isFinite(usedTokens) || !Number.isFinite(windowTokens) || usedTokens <= 0 || windowTokens <= 0) {
    return { raw: 0, display: 0 };
  }
  const rounded = Math.max(0, Math.round((usedTokens / windowTokens) * 100));
  const raw = usedTokens > windowTokens ? Math.max(101, rounded) : rounded;
  return { raw, display: Math.min(100, raw) };
}
