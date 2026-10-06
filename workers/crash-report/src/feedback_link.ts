import type { Env } from "./env";
import { ERASE_LABEL, USER_ID, accountLinkEnabled, verifyAssertion } from "./feedback_account";
import { verifyInstall } from "./feedback_auth";
import { isBlocked } from "./feedback_blocks";
import { readCappedText } from "./feedback_body";
import { accountHash, constantTimeEqual, ipPrefix, sign } from "./feedback_crypto";
import { jsonResponse, refuse, windowDetails, type FeedbackLimit } from "./feedback_http";
import { installLevel, levelProfile } from "./feedback_level";
import { take } from "./feedback_quota";
import { MINE_LIMIT, mineItems } from "./feedback_read";
import type { FeedbackRow } from "./feedback_types";

const MAX_LINKS_PER_ACCOUNT = 10;
const WRITES_PER_ACCOUNT_DAILY = 20;
const WRITES_PER_INSTALL_HOURLY = 10;
const READS_PER_ACCOUNT_HOURLY = 120;
const SHORT_ID = /^[0-9a-f]{12}$/;
const ERASE_BODY_BYTES = 512;
const NOT_ALLOWED = () => refuse("feedback.method_not_allowed", "method not allowed");

interface Caller {
  accountHash: string;
  installHash: string | null;
  now: Date;
}

// Unlike the submit path, a missing limiter refuses: these routes never run unthrottled.
// The install proof is mandatory, optional (checked only when presented) or absent.
async function authenticate(request: Request, env: Env, install: "required" | "optional" | "none"): Promise<Caller | Response> {
  if (!env.FEEDBACK_LIMITER) return refuse("feedback.account_unavailable", "account linking is unavailable");
  const now = new Date();
  const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
  if (!(await env.FEEDBACK_LIMITER.limit({ key: `link:${ipPrefix(ip)}` })).success) {
    return refuse("feedback.rate_limited", "too many requests", windowDetails("ip_hourly", now));
  }
  const account = await verifyAssertion(env, request.headers.get("x-account-assertion") ?? "", now);
  if (account instanceof Response) return account;
  const presented = request.headers.has("x-install-id") || request.headers.has("x-install-token");
  if (install === "none" || (install === "optional" && !presented)) return { accountHash: account.accountHash, installHash: null, now };
  const who = await verifyInstall(request, env);
  if (who instanceof Response) return who;
  return { accountHash: account.accountHash, installHash: who.installHash, now };
}

async function spend(env: Env, caller: Caller, steps: [string, number, FeedbackLimit][]): Promise<Response | null> {
  const day = caller.now.toISOString().slice(0, 10);
  for (const [bucket, limit, code] of steps) {
    if (!(await take(env, bucket, day, limit))) return refuse("feedback.rate_limited", "too many requests", windowDetails(code, caller.now));
  }
  return null;
}

const writeSteps = (c: Caller): [string, number, FeedbackLimit][] => [
  [`lw:${c.accountHash}:${c.now.toISOString().slice(0, 10)}`, WRITES_PER_ACCOUNT_DAILY, "link_daily"],
  ...(c.installHash ? [[`li:${c.installHash}:${c.now.toISOString().slice(0, 13)}`, WRITES_PER_INSTALL_HOURLY, "link_install_hourly"] as [string, number, FeedbackLimit]] : []),
];
const readSteps = (c: Caller): [string, number, FeedbackLimit][] => [[`lr:${c.accountHash}:${c.now.toISOString().slice(0, 13)}`, READS_PER_ACCOUNT_HOURLY, "link_read_hourly"]];

// One insert decides the cap and the one-account-per-install rule, so concurrent
// links cannot overshoot the cap or move an install.
async function createLink(request: Request, env: Env): Promise<Response> {
  const caller = await authenticate(request, env, "required");
  if (caller instanceof Response) return caller;
  const refused = await spend(env, caller, writeSteps(caller));
  if (refused) return refused;
  if (await isBlocked(env, [`install:${caller.installHash}`], caller.now)) {
    return refuse("feedback.rate_limited", "too many requests", windowDetails("link_install_hourly", caller.now));
  }
  const res = await env.DB.prepare(
    `INSERT INTO feedback_account_links (install_hash, account_hash, linked_at)
     SELECT ?, ?, ? WHERE (SELECT COUNT(*) FROM feedback_account_links WHERE account_hash = ?) < ?
     ON CONFLICT (install_hash) DO NOTHING`,
  ).bind(caller.installHash, caller.accountHash, caller.now.toISOString(), caller.accountHash, MAX_LINKS_PER_ACCOUNT).run();
  if ((res.meta?.changes ?? 0) > 0) return jsonResponse({ linked: true }, 201);
  const held = await env.DB.prepare("SELECT account_hash FROM feedback_account_links WHERE install_hash = ?").bind(caller.installHash).first<{ account_hash: string }>();
  if (held) {
    return held.account_hash === caller.accountHash ? jsonResponse({ linked: true }) : refuse("feedback.link_conflict", "this install is already linked to another account");
  }
  return refuse("feedback.link_limit", "this account has reached its linked install limit");
}

// Removal answers the same whether or not a link matched, so it cannot be used
// to learn which installs another account holds.
async function removeLink(request: Request, env: Env, shortId: string | null): Promise<Response> {
  const caller = await authenticate(request, env, shortId === null ? "required" : "none");
  if (caller instanceof Response) return caller;
  if (shortId !== null && !SHORT_ID.test(shortId)) return refuse("feedback.invalid", "install id must be the 12 character short id");
  const refused = await spend(env, caller, writeSteps(caller));
  if (refused) return refused;
  if (shortId === null) {
    await env.DB.prepare("DELETE FROM feedback_account_links WHERE install_hash = ? AND account_hash = ?").bind(caller.installHash, caller.accountHash).run();
  } else {
    await env.DB.prepare("DELETE FROM feedback_account_links WHERE account_hash = ? AND substr(install_hash, 1, 12) = ?").bind(caller.accountHash, shortId).run();
  }
  return jsonResponse({ ok: true });
}

async function listLinks(request: Request, env: Env): Promise<Response> {
  const caller = await authenticate(request, env, "optional");
  if (caller instanceof Response) return caller;
  const refused = await spend(env, caller, readSteps(caller));
  if (refused) return refused;
  const { results } = await env.DB.prepare("SELECT install_hash, linked_at FROM feedback_account_links WHERE account_hash = ? ORDER BY linked_at, install_hash")
    .bind(caller.accountHash)
    .all<{ install_hash: string; linked_at: string }>();
  return jsonResponse({ installs: results.map((r) => ({ id: r.install_hash.slice(0, 12), linkedAt: r.linked_at, current: r.install_hash === caller.installHash })) });
}

async function accountMine(request: Request, env: Env): Promise<Response> {
  const caller = await authenticate(request, env, "optional");
  if (caller instanceof Response) return caller;
  const refused = await spend(env, caller, readSteps(caller));
  if (refused) return refused;
  const { results } = await env.DB.prepare(
    `SELECT f.* FROM feedback f JOIN feedback_account_links l ON l.install_hash = f.install_hash
     WHERE l.account_hash = ? ORDER BY f.created_at DESC LIMIT ?`,
  ).bind(caller.accountHash, MINE_LIMIT).all<FeedbackRow>();
  const items = (await mineItems(env, results)).map((item, i) => ({ ...item, linkedHere: caller.installHash !== null && results[i].install_hash === caller.installHash }));
  const profile = caller.installHash ? { profile: levelProfile(await installLevel(env, caller.installHash, caller.now), caller.now) } : {};
  return jsonResponse({ ...profile, items });
}

// Called by the accounts worker when an account is deleted. The signature covers
// the raw body, and deleting by account hash makes a repeat or a retry a no-op.
export async function handleErase(request: Request, env: Env): Promise<Response> {
  const secret = env.FEEDBACK_ACCOUNT_SECRET ?? "";
  const text = await readCappedText(request, ERASE_BODY_BYTES);
  const presented = request.headers.get("x-erase-signature") ?? "";
  if (text === null || presented === "" || !(await constantTimeEqual(presented, await sign(secret, ERASE_LABEL, text)))) {
    return refuse("feedback.unauthorized", "erase signature missing or invalid");
  }
  let sub: unknown;
  try {
    sub = (JSON.parse(text) as { sub?: unknown }).sub;
  } catch {
    sub = undefined;
  }
  if (typeof sub !== "string" || !USER_ID.test(sub)) return refuse("feedback.invalid", "sub must be a numeric account id");
  await env.DB.prepare("DELETE FROM feedback_account_links WHERE account_hash = ?").bind(await accountHash(env.FEEDBACK_TOKEN_SECRET ?? "", sub)).run();
  return jsonResponse({ ok: true });
}

export function eraseEnabled(env: Env): boolean {
  return !!env.FEEDBACK_ACCOUNT_SECRET && !!env.FEEDBACK_TOKEN_SECRET;
}

// Returns null when the path is not a link route or the feature is off, so the
// caller keeps routing exactly as it did before the feature existed.
export async function handleLinkRoute(request: Request, env: Env, path: string): Promise<Response | null> {
  const method = request.method;
  if (path === "/v1/feedback/account/erase" && eraseEnabled(env)) return method === "POST" ? handleErase(request, env) : NOT_ALLOWED();
  if (!accountLinkEnabled(env)) return null;
  if (path === "/v1/feedback/link") {
    if (method === "POST") return createLink(request, env);
    if (method === "DELETE") return removeLink(request, env, null);
    return method === "GET" ? listLinks(request, env) : NOT_ALLOWED();
  }
  const short = path.match(/^\/v1\/feedback\/link\/([^/]+)$/);
  if (short) return method === "DELETE" ? removeLink(request, env, short[1]) : NOT_ALLOWED();
  if (path === "/v1/feedback/account/mine") return method === "GET" ? accountMine(request, env) : NOT_ALLOWED();
  return null;
}
