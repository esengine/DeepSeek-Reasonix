import type { Env } from "./env";
import { TRUST_DAYS, type FeedbackRow } from "./feedback_types";

const ISSUE_URL = /^https:\/\/github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)\/issues\/(\d+)$/;

export type AdoptionBasis = "shipped" | "explicit";
export type AdoptionOutcome = "credited" | "already_credited" | "item_credited" | "tombstoned" | "no_item_key";

// The canonical item is the issue the report is linked to; a stored issue whose
// url and number disagree has no key and therefore earns nothing.
export function itemKeyOf(row: Pick<FeedbackRow, "issue_number" | "issue_url">): string | null {
  const m = row.issue_url ? ISSUE_URL.exec(row.issue_url) : null;
  return m && row.issue_number !== null && Number(m[2]) === row.issue_number ? `${m[1].toLowerCase()}#${m[2]}` : null;
}

const CREDITED = "EXISTS (SELECT 1 FROM feedback_adoptions WHERE receipt = ? AND adopted_at = ?)";

// Every statement is guarded by server state read in the same transaction: the
// install comes from the stored report, never from the caller. The first
// statement is the ledger insert, so callers read its change count to learn
// whether this call credited. Trust is only renewed for an install that is
// neither revoked nor blocked; the credit itself is kept either way.
export function adoptionStatements(env: Env, row: Pick<FeedbackRow, "receipt" | "install_hash">, itemKey: string, version: string, basis: AdoptionBasis, at: string): D1PreparedStatement[] {
  const { receipt, install_hash: hash } = row;
  const [eligible, eligibleBinds] = basis === "shipped"
    ? ["EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND install_hash = ? AND status = 'fixed' AND resolved_version = ?) AND NOT EXISTS (SELECT 1 FROM feedback_adoptions WHERE item_key = ?)", [receipt, hash, version, itemKey]]
    : ["EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND install_hash = ? AND status IN ('fixed','duplicate') AND issue_number IS NOT NULL)", [receipt, hash]];
  const expiry = new Date(Date.parse(at) + TRUST_DAYS * 86_400_000).toISOString();
  return [
    env.DB.prepare(`INSERT OR IGNORE INTO feedback_adoptions (install_hash, item_key, receipt, version, adopted_at) SELECT ?, ?, ?, ?, ? WHERE ${eligible}`)
      .bind(hash, itemKey, receipt, version, at, ...eligibleBinds),
    env.DB.prepare("INSERT INTO feedback_audit (at, action, detail) SELECT ?, 'adopt', ? WHERE changes() > 0")
      .bind(at, `${receipt} install:${hash} ${itemKey} ${version} ${basis}`.slice(0, 300)),
    env.DB.prepare(
      `INSERT INTO feedback_trust (install_hash, created_at, expires_at) SELECT ?, ?, ?
       WHERE ${CREDITED}
         AND NOT EXISTS (SELECT 1 FROM feedback_level_state WHERE install_hash = ? AND revoked_at IS NOT NULL)
         AND NOT EXISTS (SELECT 1 FROM feedback_blocks WHERE target = ? AND (expires_at IS NULL OR expires_at > ?))
       ON CONFLICT (install_hash) DO UPDATE SET expires_at = MAX(feedback_trust.expires_at, excluded.expires_at)`,
    ).bind(hash, at, expiry, receipt, at, hash, `install:${hash}`, at),
    env.DB.prepare(
      `INSERT INTO feedback_level_state (install_hash, high_water_count, updated_at)
       SELECT ?, (SELECT COUNT(*) FROM feedback_adoptions WHERE install_hash = ? AND tombstoned_at IS NULL), ? WHERE ${CREDITED}
       ON CONFLICT (install_hash) DO UPDATE SET high_water_count = MAX(feedback_level_state.high_water_count, excluded.high_water_count), updated_at = excluded.updated_at`,
    ).bind(hash, hash, at, receipt, at),
  ];
}

export async function adoptionHolder(env: Env, receipt: string, itemKey: string): Promise<Exclude<AdoptionOutcome, "credited" | "no_item_key">> {
  const { results } = await env.DB.prepare("SELECT receipt, tombstoned_at FROM feedback_adoptions WHERE item_key = ?").bind(itemKey).all<{ receipt: string; tombstoned_at: string | null }>();
  const own = results.find((r) => r.receipt === receipt);
  if (own) return own.tombstoned_at === null ? "already_credited" : "tombstoned";
  return results.some((r) => r.tombstoned_at !== null) ? "tombstoned" : "item_credited";
}

// Revocation is a durable marker: trust rows are deleted on revoke, so without
// it a later credit could not tell a lapse from a revocation.
export function markRevokedStatement(env: Env, installHash: string, rejectedReceipt?: string): D1PreparedStatement {
  const at = new Date().toISOString();
  return env.DB.prepare(
    `INSERT INTO feedback_level_state (install_hash, high_water_count, revoked_at, updated_at)
     SELECT ?, 0, ?, ? WHERE (? IS NULL OR EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = 'rejected'))
     ON CONFLICT (install_hash) DO UPDATE SET revoked_at = excluded.revoked_at, updated_at = excluded.updated_at`,
  ).bind(installHash, at, at, rejectedReceipt ?? null, rejectedReceipt ?? null);
}

// Any explicit maintainer grant re-establishes trust and so ends the revocation.
export function clearRevokedStatement(env: Env, installHash: string, releasedReceipt?: string): D1PreparedStatement {
  return env.DB.prepare(
    "UPDATE feedback_level_state SET revoked_at = NULL, updated_at = ? WHERE install_hash = ? AND revoked_at IS NOT NULL AND (? IS NULL OR EXISTS (SELECT 1 FROM feedback WHERE receipt = ? AND status = 'received'))",
  ).bind(new Date().toISOString(), installHash, releasedReceipt ?? null, releasedReceipt ?? null);
}

export function tombstoneStatements(env: Env, receipt: string, installHash: string, itemKey: string): D1PreparedStatement[] {
  const at = new Date().toISOString();
  return [
    env.DB.prepare("UPDATE feedback_adoptions SET tombstoned_at = ? WHERE receipt = ? AND tombstoned_at IS NULL").bind(at, receipt),
    env.DB.prepare("INSERT INTO feedback_audit (at, action, detail) SELECT ?, 'adoption_tombstone', ? WHERE changes() > 0")
      .bind(at, `${receipt} install:${installHash} ${itemKey}`.slice(0, 300)),
  ];
}
