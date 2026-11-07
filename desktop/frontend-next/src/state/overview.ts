import type { CostQuote } from "../port/wire";
import type { SessionUsage, SourceUsage } from "../port/session";
import { quoteAmount } from "./usage";

// What a session overview says is only worth saying with its definition beside
// it, so the arithmetic lives here, once, and the card words it.

/** hit / (hit + miss - unknown): prompt the endpoint did not split into cached
 *  and uncached is out of the denominator, so it cannot pull the rate down.
 *  Null when nothing was split at all. */
export function cacheRate(hit: number, miss: number, unknown = 0): number | null {
  const known = hit + miss - unknown;
  if (!(known > 0)) return null;
  return Math.min(1, hit / known);
}

/** The session rate with its cold start taken out of hit, miss and unknown alike:
 *  the first executor request pays for the prefix every later one reuses. Null
 *  until a request after it split its prompt. */
export function warmCacheRate(u: Pick<SessionUsage, "cacheHit" | "cacheMiss" | "cacheUnknown" | "coldStart">): number | null {
  const c = u.coldStart;
  if (!c) return null;
  return cacheRate(u.cacheHit - c.cacheHit, u.cacheMiss - c.cacheMiss, (u.cacheUnknown ?? 0) - (c.cacheUnknown ?? 0));
}

/** output ÷ input tokens over the same requests. Null with no input. */
export function outputRatio(u: Pick<SessionUsage, "promptTokens" | "completionTokens">): number | null {
  return u.promptTokens > 0 ? u.completionTokens / u.promptTokens : null;
}

export type CostSay =
  | { kind: "exact"; amount: number; currency: string }
  | { kind: "atLeast"; amount: number; currency: string }
  | { kind: "unknown"; reason: string }
  | { kind: "none" };

/** The cost as its coverage allows it to be said: an exact figure only when every
 *  request carried a price fact, a floor when some did, and nothing otherwise. */
export function costSay(quote: CostQuote | undefined): CostSay {
  const coverage = quote?.coverage ?? (quote?.costComplete ? "complete" : "none");
  const amount = quoteAmount(quote);
  const currency = quote?.selected?.currency || quote?.original.currency || "";
  if (coverage === "complete" && amount !== undefined) return { kind: "exact", amount, currency };
  if (coverage === "partial" && amount !== undefined) return { kind: "atLeast", amount, currency };
  if (coverage === "incomplete") return { kind: "unknown", reason: quote?.incompleteReason ?? "" };
  return { kind: "none" };
}

export function sourceTokens(s: Pick<SourceUsage, "promptTokens" | "completionTokens">): number {
  return s.promptTokens + s.completionTokens;
}

export function sessionTokens(u: Pick<SessionUsage, "promptTokens" | "completionTokens">): number {
  return u.promptTokens + u.completionTokens;
}
