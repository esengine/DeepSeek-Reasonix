// The public "recommended" order. Every constant here is part of the published
// formula (REGISTRY-RANKING.md); changing one changes what the listing claims.
//
//   reputation = Wilson 95% lower bound of up / (up + down)
//   heat       = min(1, ln(1 + decayed) / ln(1 + HEAT_CAP))
//   decayed    = Σ over distinct anonymous install ids seen in the last WINDOW_DAYS
//                of 0.5^(age of that id's latest install / HALF_LIFE_DAYS)
//   score      = REPUTATION_WEIGHT × reputation + HEAT_WEIGHT × heat
// Heat rests on anonymous ids anyone can mint, so it carries the smaller weight.
export const REPUTATION_WEIGHT = 0.75;
export const HEAT_WEIGHT = 0.25;
export const WILSON_Z = 1.96;
export const WINDOW_DAYS = 30;
export const HALF_LIFE_DAYS = 14;
// A fixed ceiling rather than the listing's own maximum, so a package's score
// does not depend on which page, filter or search it was read under.
export const HEAT_CAP = 5000;

// Review flag: heavily and mostly down-voted packages go to the moderation
// console. Nothing is hidden automatically.
export const FLAG_MIN_DOWN = 10;
export const FLAG_MAX_APPROVAL = 0.3;

export function wilsonLowerBound(up: number, down: number, z = WILSON_Z): number {
  const n = up + down;
  if (n <= 0) return 0;
  const p = up / n;
  const z2 = z * z;
  const centre = p + z2 / (2 * n);
  const margin = z * Math.sqrt((p * (1 - p)) / n + z2 / (4 * n * n));
  return Math.max(0, (centre - margin) / (1 + z2 / n));
}

export interface DailyInstalls {
  date: string; // YYYY-MM-DD (UTC)
  count: number;
}

function dayNumber(date: string): number {
  return Math.floor(Date.parse(`${date.slice(0, 10)}T00:00:00Z`) / 86_400_000);
}

// Days outside [today - WINDOW_DAYS + 1, today] contribute nothing; a future
// date (clock skew) counts as today.
export function decayedInstalls(days: DailyInstalls[], today: string): number {
  const t = dayNumber(today);
  let sum = 0;
  for (const d of days) {
    const age = Math.max(0, t - dayNumber(d.date));
    if (!Number.isFinite(age) || age >= WINDOW_DAYS || d.count <= 0) continue;
    sum += d.count * Math.pow(0.5, age / HALF_LIFE_DAYS);
  }
  return sum;
}

export function heat(decayed: number): number {
  if (decayed <= 0) return 0;
  return Math.min(1, Math.log1p(decayed) / Math.log1p(HEAT_CAP));
}

// Rounded so the same inputs always store the same bytes.
export function recommendScore(up: number, down: number, days: DailyInstalls[], today: string): number {
  const s = REPUTATION_WEIGHT * wilsonLowerBound(up, down) + HEAT_WEIGHT * heat(decayedInstalls(days, today));
  return Math.round(s * 1e6) / 1e6;
}

export function approvalRate(up: number, down: number): number | null {
  const n = up + down;
  return n > 0 ? up / n : null;
}

export function needsReview(up: number, down: number): boolean {
  const rate = approvalRate(up, down);
  return down >= FLAG_MIN_DOWN && rate !== null && rate < FLAG_MAX_APPROVAL;
}
