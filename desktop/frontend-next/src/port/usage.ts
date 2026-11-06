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
  // A subscription's API-priced estimate for the day: not money billed, never added to cost.
  estimateCost?: Money[];
}

// One hour of a single-day window. `hour` is "09:00"; the bucket set is the
// whole day, including empty hours, so the chart keeps a stable x axis.
export interface UsageHour {
  hour: string;
  total: number;
  byModel: Record<string, number>;
  byProvider: Record<string, number>;
  requests: number;
  turns: number;
  cacheHit: number;
  cacheMiss: number;
  cost?: Money[];
  estimateCost?: Money[];
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
  // What subscription-equivalent connections would have cost at API prices. It is
  // an estimate of a plan's use, not a charge, and is kept out of cost.
  estimateCost?: Money[];
  // Set when any folded quote was priced from a fallback rate card. The
  // total is then an estimate wearing a "paid" label unless the panel says so.
  costEstimated?: boolean;
  // Set when a counted call's money is unknown (no rate card, or a quote that
  // could not price its usage). cost is then a floor, not the bill.
  costIncomplete?: boolean;
  // How many of those counted calls are under that floor, and the distinct
  // reasons the rows recorded (the kernel's incomplete-reason vocabulary).
  unpricedCalls?: number;
  incompleteReasons?: string[];
  // The denominator the unpriced count is read against: one per non-turn row
  // that carried tokens, a price, or a quote that could not price the call.
  countedCalls?: number;
  // Calls priced from an estimated/fallback rate card rather than a
  // provider-reported amount.
  estimatedCalls?: number;
  // Counted calls that recorded a price but no pricing_fingerprint.
  unnamedCardCalls?: number;
  // Counted calls that recorded no price fact at all: whether their card was
  // named was never recorded, so a zero here means a measured zero.
  unclassifiedCardCalls?: number;
  active_days: number;
  top_model: string;
  top_provider: string;
  daily: UsageDay[];
  // Present only when the range is one calendar day; the kernel resolves the
  // day's records into 24 hour buckets there.
  hourly?: UsageHour[];
  models: UsageModel[];
  providers: UsageProvider[];
}

export interface UploadDay {
  day: string;
  bytes: number;
}

export interface UploadProvider {
  provider: string;
  bytes: number;
  requests: number;
  percent: number;
}

// What model providers were sent: request bodies as they went out, before the
// connection adds its own overhead. Downloads are not counted.
export interface UploadReport {
  from: string;
  to: string;
  bytes: number;
  requests: number;
  daily: UploadDay[];
  providers: UploadProvider[];
}

export interface UploadUsagePort {
  uploadUsage(days: number): Promise<UploadReport>;
}
