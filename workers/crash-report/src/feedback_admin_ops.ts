import type { Env } from "./env";
import { listLimit, load, readJson } from "./feedback_admin_store";
import { adoptionStatements, clearRevokedStatement, itemKeyOf, markRevokedStatement, tombstoneStatements } from "./feedback_adoptions";
import { audit, auditStatement, blockKey, capOverride, putBlock, revokeTrustStatement, setCapOverride } from "./feedback_blocks";
import { jsonResponse, refuse } from "./feedback_http";
import { AdoptionBody, BlockBody, CapBody, UnblockBody } from "./feedback_schema";
import { CONCRETE_VERSION, GLOBAL_DAILY, MANUAL_TRUST_DAYS } from "./feedback_types";
import { scrubSensitiveText } from "./scrub";

const BLOCKS_LIMIT = 200;

export async function setReceiptTrust(env: Env, receipt: string, grant: boolean): Promise<Response> {
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  const now = new Date();
  const expiry = new Date(now.getTime() + MANUAL_TRUST_DAYS * 86_400_000).toISOString();
  await env.DB.batch([
    grant ? env.DB.prepare(
      `INSERT INTO feedback_trust (install_hash, created_at, expires_at) VALUES (?,?,?)
       ON CONFLICT (install_hash) DO UPDATE SET expires_at = MAX(feedback_trust.expires_at, excluded.expires_at)`,
    ).bind(row.install_hash, now.toISOString(), expiry) : revokeTrustStatement(env, row.install_hash),
    grant ? clearRevokedStatement(env, row.install_hash) : markRevokedStatement(env, row.install_hash),
    auditStatement(env, grant ? "trust" : "untrust", `${receipt} install:${row.install_hash}`),
  ]);
  return jsonResponse({ receipt, installTrusted: grant });
}

// A maintainer records an outcome the automatic path cannot: an already fixed
// report, or an independent outcome of a duplicate reporter.
export async function recordAdoption(request: Request, env: Env, receipt: string): Promise<Response> {
  const body = AdoptionBody.safeParse(await readJson(request));
  if (!body.success || !CONCRETE_VERSION.test(body.data.version)) return refuse("feedback.invalid", "version must be a concrete published version such as v1.2.3");
  const row = await load(env, receipt);
  if (!row) return refuse("feedback.not_found", "unknown receipt");
  const { version } = body.data;
  const key = itemKeyOf(row);
  if (!key || !(row.status === "duplicate" || (row.status === "fixed" && row.resolved_version === version))) {
    return refuse("feedback.bad_transition", "only a linked report fixed in that version, or a linked duplicate, can be recorded");
  }
  if ((await ownAdoption(env, receipt))?.tombstoned_at) return refuse("feedback.adoption_tombstoned", "this adoption was tombstoned");
  const res = await env.DB.batch(adoptionStatements(env, row, key, version, "explicit", new Date().toISOString()));
  return jsonResponse({ receipt, adopted: (await ownAdoption(env, receipt))?.tombstoned_at === null, credited: (res[0].meta?.changes ?? 0) > 0 });
}

async function ownAdoption(env: Env, receipt: string) {
  return env.DB.prepare("SELECT install_hash, item_key, tombstoned_at FROM feedback_adoptions WHERE receipt = ?").bind(receipt).first<{ install_hash: string; item_key: string; tombstoned_at: string | null }>();
}

// A correction keeps the row, so replay can never credit the same item again.
export async function tombstoneAdoption(env: Env, receipt: string): Promise<Response> {
  const own = await ownAdoption(env, receipt);
  if (!own) return refuse("feedback.not_found", "no adoption recorded for this receipt");
  if (own.tombstoned_at === null) await env.DB.batch(tombstoneStatements(env, receipt, own.install_hash, own.item_key));
  return jsonResponse({ receipt, adopted: false, tombstoned: true });
}

// handled = 0 replies belong to the converter and always have an issue; issue-less
// ones carry handled = 2 and are the maintainer's queue, so neither starves the other.
async function unhandledReplies(env: Env, url: URL, handled: number): Promise<Response> {
  const { results } = await env.DB.prepare(
    `SELECT r.id, r.receipt, r.body, f.issue_number FROM feedback_replies r JOIN feedback f ON f.receipt = r.receipt
     WHERE r.author = 'user' AND r.handled = ? ${handled === 0 ? "AND f.issue_number IS NOT NULL" : ""} ORDER BY r.id ASC LIMIT ?`,
  )
    .bind(handled, listLimit(url))
    .all<{ id: number; receipt: string; body: string; issue_number: number | null }>();
  return jsonResponse({ items: results.map((r) => ({ id: String(r.id), receipt: r.receipt, body: r.body, issueNumber: r.issue_number })) });
}

export const pendingReplies = (env: Env, url: URL) => unhandledReplies(env, url, 0);
export const triageReplies = (env: Env, url: URL) => unhandledReplies(env, url, 2);

export async function ackReply(env: Env, raw: string): Promise<Response> {
  const id = /^\d{1,15}$/.test(raw) ? Number(raw) : 0;
  if (id === 0) return refuse("feedback.not_found", "unknown reply");
  const res = await env.DB.prepare("UPDATE feedback_replies SET handled = 1 WHERE id = ? AND author = 'user'").bind(id).run();
  if ((res.meta?.changes ?? 0) === 0) {
    const exists = await env.DB.prepare("SELECT 1 AS x FROM feedback_replies WHERE id = ? AND author = 'user'").bind(id).first();
    if (!exists) return refuse("feedback.not_found", "unknown reply");
  }
  return jsonResponse({ id: String(id), handled: true });
}

export async function blockTarget(request: Request, env: Env): Promise<Response> {
  const body = BlockBody.safeParse(await readJson(request));
  const key = body.success ? await blockKey(env.FEEDBACK_TOKEN_SECRET ?? "", body.data.target) : null;
  if (!body.success || !key) return refuse("feedback.invalid", "target must be install:<64 hex> or ip:<address>; reason is required");
  await putBlock(env, key, scrubSensitiveText(body.data.reason), body.data.hours);
  await audit(env, "block", `${key} hours=${body.data.hours ?? "permanent"}`);
  return jsonResponse({ target: key, hours: body.data.hours ?? null });
}

export async function unblockTarget(request: Request, env: Env): Promise<Response> {
  const body = UnblockBody.safeParse(await readJson(request));
  const key = body.success ? await blockKey(env.FEEDBACK_TOKEN_SECRET ?? "", body.data.target) : null;
  if (!body.success || !key) return refuse("feedback.invalid", "target must be install:<64 hex> or ip:<address>");
  await env.DB.prepare("DELETE FROM feedback_blocks WHERE target = ?").bind(key).run();
  await audit(env, "unblock", key);
  return jsonResponse({ target: key, removed: true });
}

export async function listBlocks(env: Env): Promise<Response> {
  const { results } = await env.DB.prepare("SELECT target, reason, created_at, expires_at FROM feedback_blocks WHERE expires_at IS NULL OR expires_at > ? LIMIT ?")
    .bind(new Date().toISOString(), BLOCKS_LIMIT)
    .all<{ target: string; reason: string; created_at: string; expires_at: string | null }>();
  return jsonResponse({ items: results.map((r) => ({ target: r.target, reason: r.reason, createdAt: r.created_at, expiresAt: r.expires_at })) });
}

export async function setCap(request: Request, env: Env): Promise<Response> {
  const body = CapBody.safeParse(await readJson(request));
  if (!body.success) return refuse("feedback.invalid", "dailyGlobal must be an integer from 0 to 5000");
  await setCapOverride(env, body.data.dailyGlobal);
  await audit(env, "cap", `dailyGlobal=${body.data.dailyGlobal}`);
  return jsonResponse({ dailyGlobal: body.data.dailyGlobal });
}

export async function getCap(env: Env): Promise<Response> {
  const override = await capOverride(env);
  return jsonResponse({ dailyGlobal: override ?? GLOBAL_DAILY, overridden: override !== null });
}
