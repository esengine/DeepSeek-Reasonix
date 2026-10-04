import type { Env } from "./env";
import { ipHash } from "./feedback_crypto";
import { AUTO_BLOCK_REJECTIONS, AUTO_BLOCK_WINDOW_DAYS, AUTO_BLOCK_HOURS, TRUST_DAYS, TRUST_ESTABLISHED_DAYS, TRUST_ESTABLISHED_RELEASES } from "./feedback_types";

const INSTALL_TARGET = /^install:[0-9a-f]{64}$/;
const IPV4 = /^(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)$/;
const IPV6 = /^[0-9a-fA-F:]{2,39}$/;
const HOUR_MS = 3_600_000;

export async function audit(env: Env, action: string, detail: string): Promise<void> {
  await env.DB.prepare("INSERT INTO feedback_audit (at, action, detail) VALUES (?, ?, ?)").bind(new Date().toISOString(), action, detail.slice(0, 300)).run();
}

// Blocks are keyed by install hash or by the keyed hash of the IP (/64 for
// IPv6), so a stored block never holds a raw address.
export async function blockKey(secret: string, target: string): Promise<string | null> {
  if (INSTALL_TARGET.test(target)) return target;
  if (!target.startsWith("ip:")) return null;
  const addr = target.slice(3).replace(/\/64$/, "");
  const ok = IPV4.test(addr) || (addr.includes(":") && IPV6.test(addr));
  return ok ? `ip:${await ipHash(secret, addr)}` : null;
}

export async function isBlocked(env: Env, keys: string[], now: Date): Promise<boolean> {
  const marks = keys.map(() => "?").join(",");
  const row = await env.DB.prepare(`SELECT 1 AS x FROM feedback_blocks WHERE target IN (${marks}) AND (expires_at IS NULL OR expires_at > ?) LIMIT 1`)
    .bind(...keys, now.toISOString())
    .first();
  return row !== null;
}

export async function putBlock(env: Env, key: string, reason: string, hours: number | undefined): Promise<void> {
  const now = new Date();
  const expires = hours === undefined ? null : new Date(now.getTime() + hours * HOUR_MS).toISOString();
  await env.DB.prepare(
    "INSERT INTO feedback_blocks (target, reason, created_at, expires_at) VALUES (?,?,?,?) ON CONFLICT (target) DO UPDATE SET reason = excluded.reason, created_at = excluded.created_at, expires_at = excluded.expires_at",
  )
    .bind(key, reason, now.toISOString(), expires)
    .run();
}

// An automatic block only ever extends a block: a longer or permanent manual one survives.
// It is a statement so it commits with the rejection that triggers it.
export function autoBlockStatement(env: Env, installHash: string): D1PreparedStatement {
  const now = new Date();
  const since = new Date(now.getTime() - AUTO_BLOCK_WINDOW_DAYS * 86_400_000).toISOString();
  return env.DB.prepare(
    `INSERT INTO feedback_blocks (target, reason, created_at, expires_at)
     SELECT ?, ?, ?, ? WHERE (SELECT COUNT(*) FROM feedback WHERE install_hash = ? AND status = 'rejected' AND updated_at >= ?) >= ?
     ON CONFLICT (target) DO UPDATE SET reason = excluded.reason, created_at = excluded.created_at, expires_at = excluded.expires_at
     WHERE feedback_blocks.expires_at IS NOT NULL AND feedback_blocks.expires_at < excluded.expires_at`,
  ).bind(
    `install:${installHash}`,
    `auto: ${AUTO_BLOCK_REJECTIONS} rejected in ${AUTO_BLOCK_WINDOW_DAYS} days`,
    now.toISOString(),
    new Date(now.getTime() + AUTO_BLOCK_HOURS * HOUR_MS).toISOString(),
    installHash,
    since,
    AUTO_BLOCK_REJECTIONS,
  );
}

export function auditStatement(env: Env, action: string, detail: string): D1PreparedStatement {
  return env.DB.prepare("INSERT INTO feedback_audit (at, action, detail) VALUES (?, ?, ?)").bind(new Date().toISOString(), action, detail.slice(0, 300));
}

const CAP_KEY = "global_daily_cap";

export async function capOverride(env: Env): Promise<number | null> {
  const row = await env.DB.prepare("SELECT value FROM feedback_config WHERE key = ?").bind(CAP_KEY).first<{ value: number }>();
  return row ? row.value : null;
}

export async function setCapOverride(env: Env, value: number): Promise<void> {
  await env.DB.prepare("INSERT INTO feedback_config (key, value, updated_at) VALUES (?,?,?) ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at")
    .bind(CAP_KEY, value, new Date().toISOString())
    .run();
}

export async function isTrusted(env: Env, installHash: string, now = new Date()): Promise<boolean> {
  return (await env.DB.prepare("SELECT 1 AS x FROM feedback_trust WHERE install_hash = ? AND expires_at > ?").bind(installHash, now.toISOString()).first()) !== null;
}

// The durable ledger counts explicit releases once even after report retention.
export function releaseLedgerStatement(env: Env, installHash: string, receipt: string, at: string): D1PreparedStatement {
  return env.DB.prepare(
    "INSERT OR IGNORE INTO feedback_releases (receipt, install_hash, released_at) SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND install_hash = ? AND status = 'received')",
  ).bind(receipt, installHash, at, receipt, installHash);
}

export function grantTrustStatement(env: Env, installHash: string, guardReceipt: string): D1PreparedStatement {
  const now = new Date();
  return env.DB.prepare(
    `INSERT INTO feedback_trust (install_hash, created_at, expires_at)
     SELECT ?, ?, CASE WHEN (SELECT COUNT(*) FROM feedback_releases WHERE install_hash = ?) >= ? THEN ? ELSE ? END
     WHERE EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND install_hash = ? AND status = 'received')
     ON CONFLICT (install_hash) DO UPDATE SET expires_at = MAX(feedback_trust.expires_at, excluded.expires_at)`,
  ).bind(installHash, now.toISOString(), installHash, TRUST_ESTABLISHED_RELEASES,
    new Date(now.getTime() + TRUST_ESTABLISHED_DAYS * 86_400_000).toISOString(),
    new Date(now.getTime() + TRUST_DAYS * 86_400_000).toISOString(), guardReceipt, installHash);
}

export function revokeTrustStatement(env: Env, installHash: string): D1PreparedStatement {
  return env.DB.prepare("DELETE FROM feedback_trust WHERE install_hash = ?").bind(installHash);
}
