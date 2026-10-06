import type { Env } from "./env";
import { MAX_REPLIES_PER_ITEM, RESERVED_SHARE } from "./feedback_types";
import { refuse, windowDetails, type FeedbackLimit } from "./feedback_http";
import { ipPrefix } from "./feedback_crypto";
import { ownedByCaller } from "./feedback_ownership";

export async function ipLimited(env: Env, ip: string, trusted: boolean): Promise<boolean> {
  return !trusted && !!env.FEEDBACK_LIMITER && !(await env.FEEDBACK_LIMITER.limit({ key: ipPrefix(ip) })).success;
}

// Atomically takes one unit from a fixed-window counter; false once `limit` is reached.
export async function take(env: Env, bucket: string, day: string, limit: number): Promise<boolean> {
  if (limit <= 0) return false;
  const row = await env.DB.prepare(
    "INSERT INTO feedback_quota (bucket, n, day) VALUES (?, 1, ?) ON CONFLICT (bucket) DO UPDATE SET n = n + 1 WHERE n < ? RETURNING n",
  )
    .bind(bucket, day, limit)
    .first<{ n: number }>();
  return row !== null;
}

export interface QuotaKeys {
  ipKey: string;
  installHash: string;
}

export interface QuotaLimits {
  globalDaily: number;
  ipHourly: number;
  installHourly: number;
  installDaily: number;
  trusted: boolean;
}

export type Admission = { kind: "ok"; buckets: string[] } | { kind: "refused"; response: Response; limit?: FeedbackLimit };

type ReplyPolicy = { kind: "reply"; trusted: boolean; hourly: number; receipt: string; replyable: readonly string[] };
type AdmissionPolicy = { kind: "submit"; limits: QuotaLimits } | ReplyPolicy;

function callerSteps(keys: QuotaKeys, now: Date, limits: QuotaLimits): [string, number, FeedbackLimit][] {
  const day = now.toISOString().slice(0, 10);
  const hour = now.toISOString().slice(0, 13);
  return [
    ...(!limits.trusted ? [[`ip:${keys.ipKey}:${hour}`, limits.ipHourly, "ip_hourly"] as [string, number, FeedbackLimit]] : []),
    [`ih:${keys.installHash}:${hour}`, limits.installHourly, "install_hourly"],
    [`id:${keys.installHash}:${day}`, limits.installDaily, "install_daily"],
  ];
}

export async function refund(env: Env, buckets: readonly string[]): Promise<void> {
  if (buckets.length === 0) return;
  await env.DB.batch(buckets.map((bucket) => env.DB.prepare("UPDATE feedback_quota SET n = n - 1 WHERE bucket = ? AND n > 0").bind(bucket)));
}

export async function replyRefusal(env: Env, installHash: string, policy: ReplyPolicy, now: Date): Promise<Response | null> {
  const owner = ownedByCaller(env, installHash, now);
  const row = await env.DB.prepare(
    `SELECT status, (SELECT COUNT(*) FROM feedback_replies WHERE receipt = ? AND author = 'user') AS replies FROM feedback WHERE receipt = ? AND ${owner.sql}`,
  ).bind(policy.receipt, policy.receipt, ...owner.binds).first<{ status: string; replies: number }>();
  if (!row || !policy.replyable.includes(row.status)) return refuse("feedback.not_replyable", "this report cannot take replies");
  return row.replies >= MAX_REPLIES_PER_ITEM ? refuse("feedback.reply_limit", "reply limit reached for this report", windowDetails("reply_item", now)) : null;
}

// Untrusted installs stop short of the global cap so the last tenth stays
// available to installs whose earlier reports were accepted.
export function untrustedShare(globalDaily: number): number {
  return Math.floor(globalDaily * (1 - RESERVED_SHARE));
}

// Every caller follows the same refusal precedence; blocked callers inspect
// counters without spending them and conceal the block only after all gates.
export async function admit(env: Env, keys: QuotaKeys, now: Date, policy: AdmissionPolicy, blocked: boolean, ip: string): Promise<Admission> {
  const day = now.toISOString().slice(0, 10);
  const trusted = policy.kind === "submit" ? policy.limits.trusted : policy.trusted;
  const limited = (code: FeedbackLimit): Admission => ({
    kind: "refused", limit: code,
    response: refuse("feedback.rate_limited", policy.kind === "submit" ? "submission limit reached" : "reply limit reached", windowDetails(code, now)),
  });
  const busy = (code: "global_burst" | "global_daily"): Admission => ({
    kind: "refused", limit: code,
    response: refuse("feedback.busy", code === "global_burst" ? "feedback is busy, try again later" : "feedback is busy, try again tomorrow", windowDetails(code, now)),
  });
  if (await ipLimited(env, ip, trusted)) return limited("ip_hourly");
  if (policy.kind === "submit" && env.FEEDBACK_BUDGET_LIMITER && !(await env.FEEDBACK_BUDGET_LIMITER.limit({ key: "global" })).success) return busy("global_burst");
  const steps: [string, number, FeedbackLimit][] = policy.kind === "submit" ? [
    ...callerSteps(keys, now, policy.limits),
    [`g:${day}`, trusted ? policy.limits.globalDaily : untrustedShare(policy.limits.globalDaily), "global_daily"],
  ] : [[`rh:${keys.installHash}:${now.toISOString().slice(0, 13)}`, policy.hourly, "reply_hourly"]];
  const taken: string[] = [];
  try {
    for (const [bucket, limit, code] of steps) {
      const available = blocked
        ? ((await env.DB.prepare("SELECT n FROM feedback_quota WHERE bucket = ?").bind(bucket).first<{ n: number }>())?.n ?? 0) < limit
        : await take(env, bucket, day, limit);
      if (!available) {
        await refund(env, taken);
        return code === "global_daily" ? busy(code) : limited(code);
      }
      if (!blocked) taken.push(bucket);
    }
    if (policy.kind === "reply") {
      const response = await replyRefusal(env, keys.installHash, policy, now);
      if (response) {
        await refund(env, taken);
        return { kind: "refused", response };
      }
    }
  } catch (err) {
    await refund(env, taken);
    throw err;
  }
  if (blocked) return limited(policy.kind === "reply" && !trusted ? "ip_hourly" : steps[0][2]);
  return { kind: "ok", buckets: taken };
}

// True only for the first caller of the day, so an exhausted budget alerts once.
export async function firstBusyOfDay(env: Env, now: Date): Promise<boolean> {
  const day = now.toISOString().slice(0, 10);
  return take(env, `alert:${day}`, day, 1);
}
