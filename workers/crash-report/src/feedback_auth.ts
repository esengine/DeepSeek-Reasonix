import type { Env } from "./env";
import { constantTimeEqual, installHash, installToken, ipHash } from "./feedback_crypto";
import { refuse, windowDetails } from "./feedback_http";
import { take } from "./feedback_quota";

// Endpoints stay off until both secrets exist, so a half-configured deploy never
// mints tokens the converter cannot be authorised against.
export function feedbackEnabled(env: Env): boolean {
  return env.FEEDBACK_ENABLED !== "false" && !!env.FEEDBACK_TOKEN_SECRET && !!env.FEEDBACK_ADMIN_TOKEN;
}

const INSTALL_ID = /^[A-Za-z0-9_-]{16,64}$/;

export async function tokenMatches(secret: string, installId: string, presented: string): Promise<boolean> {
  return presented !== "" && (await constantTimeEqual(presented, await installToken(secret, installId)));
}

// Trust and release evidence can outlive reports; neither proves the caller owns
// the install. Retained identity evidence still requires the original token.
export async function isKnownInstall(env: Env, hash: string): Promise<boolean> {
  return (await env.DB.prepare(
    `SELECT 1 AS x WHERE EXISTS (SELECT 1 FROM feedback WHERE install_hash = ?)
       OR EXISTS (SELECT 1 FROM feedback_trust WHERE install_hash = ?)
       OR EXISTS (SELECT 1 FROM feedback_releases WHERE install_hash = ?)
       OR EXISTS (SELECT 1 FROM feedback_adoptions WHERE install_hash = ?)`,
  ).bind(hash, hash, hash, hash).first()) !== null;
}

export async function verifyInstall(request: Request, env: Env): Promise<{ installHash: string } | Response> {
  if (!feedbackEnabled(env) || !env.FEEDBACK_TOKEN_SECRET) return refuse("feedback.disabled", "feedback is unavailable");
  const id = request.headers.get("x-install-id") ?? "";
  const token = request.headers.get("x-install-token") ?? "";
  if (!INSTALL_ID.test(id) || !(await tokenMatches(env.FEEDBACK_TOKEN_SECRET, id, token))) {
    return refuse("feedback.bad_token", "install token missing or invalid");
  }
  return { installHash: await installHash(env.FEEDBACK_TOKEN_SECRET, id) };
}

const LOCKOUT_FAILURES = 5;
const LOCKOUT_WINDOW_MS = 15 * 60_000;

// Fixed windows keyed by the keyed hash of the caller, so no raw address is stored.
async function lockoutBucket(request: Request, env: Env, now: Date): Promise<{ bucket: string; day: string }> {
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  const window = Math.floor(now.getTime() / LOCKOUT_WINDOW_MS);
  return { bucket: `af:${await ipHash(env.FEEDBACK_TOKEN_SECRET ?? "admin-lockout", ip)}:${window}`, day: now.toISOString().slice(0, 10) };
}

// Only the Authorization header carries the token, never the URL. A slot is taken
// before the compare and handed back on success, so concurrent wrong tokens cannot
// all pass the check; once the slots are spent even the right token gets 429.
export async function requireAdmin(request: Request, env: Env): Promise<Response | null> {
  const header = request.headers.get("authorization") ?? "";
  const presented = header.startsWith("Bearer ") ? header.slice(7) : "";
  if (!presented) return refuse("feedback.unauthorized", "admin token missing or invalid");
  const now = new Date();
  const { bucket, day } = await lockoutBucket(request, env, now);
  if (!(await take(env, bucket, day, LOCKOUT_FAILURES))) return refuse("feedback.rate_limited", "too many failed attempts, try again later", windowDetails("admin_attempts", now));
  if (env.FEEDBACK_ADMIN_TOKEN && (await constantTimeEqual(presented, env.FEEDBACK_ADMIN_TOKEN))) {
    await env.DB.prepare("UPDATE feedback_quota SET n = n - 1 WHERE bucket = ? AND n > 0").bind(bucket).run();
    return null;
  }
  return refuse("feedback.unauthorized", "admin token missing or invalid");
}
