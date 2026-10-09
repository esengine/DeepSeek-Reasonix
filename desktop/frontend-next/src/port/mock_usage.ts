import type { UsageQuery, UsageReport } from "./port";
import { USAGE_PRICED, USAGE_TOKENS } from "./mock_memory";
import { DEFAULT_USAGE_DAYS } from "./usage";

const DAY_MS = 864e5;

function parseDay(value: string): number {
  const parsed = Date.parse(`${value}T00:00:00Z`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(parsed) || new Date(parsed).toISOString().slice(0, 10) !== value) {
    throw new Error("range dates must be YYYY-MM-DD");
  }
  return parsed;
}

function explicitRange(query: Extract<UsageQuery, { from: string }>) {
  const from = parseDay(query.from);
  const to = parseDay(query.to);
  const days = Math.round((to - from) / DAY_MS) + 1;
  if (days < 1 || days > 365) throw new Error("range must be between 1 and 365 days");
  return { from, days };
}

// A fortnight with the shape a real one has: a couple of heavy days, a
// quiet stretch, and an early span the recorder priced before cost was
// persisted — those days carry tokens and no cost, which the panel must not
// render as free.
export function mockUsage(query: number | UsageQuery): UsageReport {
  const shape = USAGE_TOKENS;
  const priced = USAGE_PRICED;
  const explicit = typeof query === "number" || "days" in query ? null : explicitRange(query);
  const days = explicit?.days ?? (typeof query === "number" ? query : "days" in query ? query.days : DEFAULT_USAGE_DAYS);
  if (days < 1 || days > 365) throw new Error("days must be between 1 and 365");
  const count = explicit ? days : Math.min(days, shape.length);
  // Index against the full arrays, not the slice: taking 7 of 13 days shifted
  // both the dates and the cost column by six.
  const first = explicit ? 0 : shape.length - count;
  const daily = Array.from({ length: count }, (_, i) => {
    const fixture = explicit ? i % shape.length : first + i;
    const total = shape[fixture];
    const at = (explicit ? new Date(explicit.from + i * DAY_MS) : new Date(Date.now() - (count - 1 - i) * DAY_MS)).toISOString().slice(0, 10);
    const amount = priced[fixture];
    return {
      day: at, total,
      byModel: (total ? { "deepseek/deepseek-flash": total } : {}) as Record<string, number>,
      byProvider: (total ? { deepseek: total } : {}) as Record<string, number>,
      requests: Math.round(total / 20_000), turns: Math.round(total / 150_000),
      cacheHit: Math.round(total * 0.92), cacheMiss: Math.round(total * 0.08),
      cost: amount ? [{ amount, currency: "CNY" }] : undefined,
    };
  });
  const tokens = daily.reduce((a, d) => a + d.total, 0);
  return {
    from: daily[0]?.day ?? "", to: daily.at(-1)?.day ?? "",
    tokens, requests: daily.reduce((a, d) => a + d.requests, 0),
    turns: daily.reduce((a, d) => a + d.turns, 0),
    cache_hit: daily.reduce((a, d) => a + d.cacheHit, 0),
    cache_miss: daily.reduce((a, d) => a + d.cacheMiss, 0),
    cost: [{ amount: "10.4882", currency: "CNY" }],
    active_days: daily.filter((d) => d.total > 0).length,
    top_model: "deepseek/deepseek-flash", top_provider: "deepseek",
    daily,
    models: [
      { model: "deepseek/deepseek-flash", provider: "deepseek", tokens: Math.round(tokens * 0.597), percent: 59.7 },
      { model: "deepseek-flash/deepseek-flash", provider: "deepseek-flash", tokens: Math.round(tokens * 0.401), percent: 40.1 },
      { model: "deepseek/deepseek-v4-pro", provider: "deepseek", tokens: Math.round(tokens * 0.002), percent: 0.2 },
    ],
    providers: [
      { provider: "deepseek", tokens: Math.round(tokens * 0.599), percent: 59.9 },
      { provider: "deepseek-flash", tokens: Math.round(tokens * 0.401), percent: 40.1 },
    ],
  };
}
