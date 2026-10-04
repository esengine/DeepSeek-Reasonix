// What this machine has spent — tokens on one side, money on the other.
//
// Cost is a list rather than a number because two billing currencies never add
// up: a single total would have to invent an exchange rate, and the rate a turn
// was billed at is not the rate today. An empty list means the records carry no
// cost for that span, which is not the same as having cost nothing.
// A usage window is either a trailing day count or an explicit inclusive
// calendar range. Keeping them separate prevents "from" from silently riding
// on a days query and being ignored by older kernels.
export const DEFAULT_USAGE_DAYS = 30;

export type UsageQuery =
  | { days: number; source?: string }
  | { from: string; to: string; source?: string };

export interface Money {
  amount: string;
  currency: string;
}

export interface UsageDay {
  day: string;
  total: number;
  byModel: Record<string, number>;
  byProvider: Record<string, number>;
  requests: number;
  turns: number;
  cacheHit: number;
  cacheMiss: number;
  cost?: Money[];
}

export interface UsageModel {
  model: string;
  provider: string;
  tokens: number;
  percent: number;
}

export interface UsageProvider {
  provider: string;
  tokens: number;
  percent: number;
}

export interface UsageReport {
  from: string;
  to: string;
  tokens: number;
  requests: number;
  turns: number;
  cache_hit: number;
  cache_miss: number;
  cost?: Money[];
  // Set when any folded quote was priced from a fallback rate card. The
  // total is then an estimate wearing a "paid" label unless the panel says so.
  costEstimated?: boolean;
  active_days: number;
  top_model: string;
  top_provider: string;
  daily: UsageDay[];
  models: UsageModel[];
  providers: UsageProvider[];
}
