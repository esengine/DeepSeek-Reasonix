import type { Bindings } from "../env";
import { RateCounterRepo } from "../db/rateCounters";
import { ApiError } from "./errors";

export interface RateRule {
  name: string;
  limit: number;
  windowMs: number;
}

function unavailable(): ApiError {
  return new ApiError(503, "rate_limit_unavailable", "This action is temporarily unavailable.");
}

// Counts one attempt against `subject` and refuses past the limit. Unlike the
// per-IP binding limiter, a missing database, a missing subject or a failed
// write refuses the request rather than letting it through.
export async function enforceRateLimit(env: Pick<Bindings, "DB">, rule: RateRule, subject: string): Promise<void> {
  if (!subject) throw unavailable();
  let hit: { count: number; resetsAt: number };
  try {
    hit = await new RateCounterRepo(env.DB).hit(`${rule.name}:${subject}`, rule.windowMs, Date.now());
  } catch {
    throw unavailable();
  }
  if (hit.count > rule.limit) {
    const retryAfterSeconds = Math.max(1, Math.ceil((hit.resetsAt - Date.now()) / 1000));
    throw new ApiError(429, "rate_limited", "Too many attempts. Please wait and try again.", { retryAfterSeconds });
  }
}

// True once `subject` has reached the rule's limit in the current window,
// without counting a new attempt.
export async function rateLimitReached(env: Pick<Bindings, "DB">, rule: RateRule, subject: string): Promise<boolean> {
  if (!subject) throw unavailable();
  try {
    return (await new RateCounterRepo(env.DB).peek(`${rule.name}:${subject}`, rule.windowMs, Date.now())) >= rule.limit;
  } catch {
    throw unavailable();
  }
}

export async function recordRateEvent(env: Pick<Bindings, "DB">, rule: RateRule, subject: string): Promise<void> {
  if (!subject) throw unavailable();
  try {
    await new RateCounterRepo(env.DB).hit(`${rule.name}:${subject}`, rule.windowMs, Date.now());
  } catch {
    throw unavailable();
  }
}
