import type { UploadReport, UsageHour, UsageQuery, UsageReport } from "./port";
import { USAGE_PRICED, USAGE_TOKENS } from "./mock_memory";
import { DEFAULT_USAGE_DAYS } from "./usage";

const DAY_MS = 864e5;

const HOUR_WEIGHTS = [0, 0, 0, 0, 0, 0, 0, 1, 2, 4, 5, 5, 4, 3, 5, 6, 4, 3, 2, 2, 1, 0, 0, 0];

// The fixture's single-day window gets the same hour shape the kernel sends.
function mockHourly(day: UsageReport["daily"][number]): UsageHour[] {
  const sum = HOUR_WEIGHTS.reduce((a, b) => a + b, 0);
  const amount = Number(day.cost?.[0]?.amount ?? 0);
  const currency = day.cost?.[0]?.currency ?? "CNY";
  const buckets = HOUR_WEIGHTS.map((weight, hour) => {
    const total = Math.round((day.total * weight) / sum);
    const byModel: Record<string, number> = total ? { "deepseek/deepseek-flash": total } : {};
    const byProvider: Record<string, number> = total ? { deepseek: total } : {};
    return {
      hour: `${String(hour).padStart(2, "0")}:00`,
      total,
      byModel,
      byProvider,
      requests: Math.round(total / 20_000),
      turns: Math.round(total / 150_000),
      cacheHit: Math.round(total * 0.92),
      cacheMiss: Math.round(total * 0.08),
      cost: total && amount ? [{ amount: ((amount * weight) / sum).toFixed(4), currency }] : undefined,
    };
  });
  // Rounding each bucket must not change the day's total the card reports.
  const delta = day.total - buckets.reduce((a, b) => a + b.total, 0);
  if (delta !== 0) {
    const at = HOUR_WEIGHTS.indexOf(Math.max(...HOUR_WEIGHTS));
    buckets[at].total += delta;
    buckets[at].byModel["deepseek/deepseek-flash"] = (buckets[at].byModel["deepseek/deepseek-flash"] ?? 0) + delta;
    buckets[at].byProvider.deepseek = (buckets[at].byProvider.deepseek ?? 0) + delta;
  }
  return buckets;
}

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
      estimateCost: i >= count - 2 && total > 0 ? [{ amount: i === count - 1 ? "2.8" : "3.5", currency: "CNY" }] : undefined,
    };
  });
  const tokens = daily.reduce((a, d) => a + d.total, 0);
  const hourly = count === 1 && daily[0] ? mockHourly(daily[0]) : undefined;
  return {
    from: daily[0]?.day ?? "", to: daily.at(-1)?.day ?? "",
    tokens, requests: daily.reduce((a, d) => a + d.requests, 0),
    turns: daily.reduce((a, d) => a + d.turns, 0),
    cache_hit: daily.reduce((a, d) => a + d.cacheHit, 0),
    cache_miss: daily.reduce((a, d) => a + d.cacheMiss, 0),
    cost: [{ amount: "10.4882", currency: "CNY" }],
    estimateCost: daily.some((d) => d.estimateCost) ? [{ amount: "6.3", currency: "CNY" }] : undefined,
    active_days: daily.filter((d) => d.total > 0).length,
    top_model: "deepseek/deepseek-flash", top_provider: "deepseek",
    daily,
    hourly,
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

// A fortnight of uploads with quiet days, two providers and one heavy day.
export function mockUploadUsage(days: number): UploadReport {
  const shown = Math.min(days, 14);
  const daily = Array.from({ length: shown }, (_, i) => ({
    day: new Date(Date.now() - (shown - 1 - i) * DAY_MS).toISOString().slice(0, 10),
    bytes: [0, 18e6, 240e6, 410e6, 95e6, 0, 0, 120e6, 760e6, 330e6, 40e6, 0, 210e6, 150e6][i] ?? 0,
  }));
  const total = daily.reduce((n, d) => n + d.bytes, 0);
  return {
    from: daily[0]?.day ?? "", to: daily[daily.length - 1]?.day ?? "", bytes: total, requests: 1280, daily,
    providers: [
      { provider: "deepseek", bytes: Math.round(total * 0.86), requests: 1100, percent: 86 },
      { provider: "relay-work", bytes: Math.round(total * 0.14), requests: 180, percent: 14 },
    ],
  };
}
