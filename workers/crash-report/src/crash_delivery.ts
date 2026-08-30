import type { Env } from "./env";
import { firebaseConfigured } from "./firebase_rtdb";

export const FIREBASE_OUTBOX_LIMIT = 5_000;
export const FIREBASE_OUTBOX_BATCH = 200;
const FIREBASE_GROUP_LEASE_MS = 60_000;
export const FIREBASE_STORAGE_BUDGET_BYTES = 700 * 1024 * 1024;
export const FIREBASE_STORAGE_WARNING_BYTES = Math.floor(FIREBASE_STORAGE_BUDGET_BYTES * 0.8);
export const FIREBASE_ACTIVE_RESERVATION_BYTES = 640 * 1024;
export const FIREBASE_COMPACTED_RESERVATION_BYTES = 128 * 1024;
export const FIREBASE_ARCHIVING_RESERVATION_BYTES = 32 * 1024;

export type FirebaseSampleState = "active" | "compacted" | "archiving" | "archived";

export type FirebaseGroupState = {
  fingerprint: string;
  sample_state: FirebaseSampleState;
  sample_epoch: number;
  epoch_first_event_id: string;
  reserved_bytes: number;
  last_seen: string;
  compacted_at: string;
  archived_at: string;
  archive_reason: "" | "retention" | "admin";
  lease_owner: string;
  lease_generation: number;
  lease_expires_at: string;
};

export type FirebaseGroupLease = { owner: string; generation: number };

export type CrashStorageMode = "d1" | "dual" | "firebase";

export type FirebaseOutboxRow = {
  event_id: string;
  fingerprint: string;
  payload: string;
  state: "queued" | "processing" | "projected";
  attempts: number;
  next_attempt_at: string;
  created_at: string;
  updated_at: string;
};

export type FirebaseProjectionReceipt = {
  group_count: number;
  latest_slot: number;
  first_sample: number;
};

export function crashStorageMode(env: Env): CrashStorageMode {
  const raw = env.CRASH_STORAGE_MODE?.trim().toLowerCase() || "d1";
  if (raw === "d1" || raw === "dual" || raw === "firebase") return raw;
  throw new Error(`invalid crash storage mode ${raw}`);
}

export function firebaseStorageReady(env: Env): boolean {
  return crashStorageMode(env) === "d1" || firebaseConfigured(env);
}

export async function enqueueFirebaseCrash(
  env: Env,
  eventId: string,
  fingerprint: string,
  payload: string,
  now: string,
): Promise<"inserted" | "duplicate" | "full"> {
  const result = await env.DB.prepare(
    `INSERT OR IGNORE INTO firebase_crash_outbox (
       event_id, fingerprint, payload, state, attempts, next_attempt_at, created_at, updated_at
     )
     SELECT ?1, ?2, ?3, 'queued', 0, ?4, ?4, ?4
     WHERE (SELECT COUNT(*) FROM firebase_crash_outbox) < ?5
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_receipts WHERE event_id = ?1)`,
  ).bind(eventId, fingerprint, payload, now, FIREBASE_OUTBOX_LIMIT).run();
  if (Number(result.meta?.changes ?? 0) > 0) return "inserted";
  const existing = await env.DB.prepare(
    `SELECT event_id FROM firebase_crash_outbox WHERE event_id = ?1
     UNION ALL SELECT event_id FROM firebase_crash_receipts WHERE event_id = ?1 LIMIT 1`,
  ).bind(eventId).first<{ event_id: string }>();
  return existing ? "duplicate" : "full";
}

export async function claimFirebaseCrash(env: Env, eventId: string, now: string): Promise<boolean> {
  const result = await env.DB.prepare(
    `UPDATE firebase_crash_outbox SET state = 'processing', updated_at = ?2
     WHERE event_id = ?1 AND (
       state = 'queued' OR (state = 'processing' AND datetime(updated_at) < datetime('now', '-10 minutes'))
     )`,
  ).bind(eventId, now).run();
  return Number(result.meta?.changes ?? 0) > 0;
}

export function projectionCompletionStatements(
  db: D1Database,
  eventId: string,
  fingerprint: string,
  now: string,
): D1PreparedStatement[] {
  const stateWrite = db.prepare(
      `UPDATE firebase_crash_group_state
       SET epoch_first_event_id = CASE WHEN epoch_first_event_id = '' THEN ?1 ELSE epoch_first_event_id END,
           last_seen = CASE WHEN last_seen < ?2 THEN ?2 ELSE last_seen END
       WHERE fingerprint = ?3 AND sample_state = 'active'`,
    ).bind(eventId, now, fingerprint);
  const receipt = db.prepare(
      `INSERT OR IGNORE INTO firebase_crash_receipts (
         event_id, projected_at, group_count, latest_slot, first_sample
       )
       SELECT ?1, ?2, groups.count, (groups.count - 1) % 5,
              CASE WHEN state.epoch_first_event_id = ?1 THEN 1 ELSE 0 END
       FROM groups
       JOIN firebase_crash_group_state AS state USING (fingerprint)
       WHERE groups.fingerprint = ?3`,
    ).bind(eventId, now, fingerprint);
  return [
    stateWrite,
    receipt,
    db.prepare(
      "UPDATE firebase_crash_outbox SET state = 'projected', updated_at = ?2 WHERE event_id = ?1",
    ).bind(eventId, now),
  ];
}

export async function firebaseProjectionReceipt(
  env: Env,
  eventId: string,
): Promise<FirebaseProjectionReceipt | null> {
  return env.DB.prepare(
    "SELECT group_count, latest_slot, first_sample FROM firebase_crash_receipts WHERE event_id = ?1",
  ).bind(eventId).first<FirebaseProjectionReceipt>();
}

export async function firebaseProjectionExists(env: Env, eventId: string): Promise<boolean> {
  return Boolean(await env.DB.prepare(
    "SELECT event_id FROM firebase_crash_receipts WHERE event_id = ?1",
  ).bind(eventId).first());
}

export async function firebaseOutboxExists(env: Env, eventId: string): Promise<boolean> {
  return Boolean(await env.DB.prepare(
    "SELECT event_id FROM firebase_crash_outbox WHERE event_id = ?1",
  ).bind(eventId).first());
}

export async function firebaseEventExists(env: Env, eventId: string): Promise<boolean> {
  return Boolean(await env.DB.prepare(
    `SELECT event_id FROM firebase_crash_outbox WHERE event_id = ?1
     UNION ALL SELECT event_id FROM firebase_crash_receipts WHERE event_id = ?1 LIMIT 1`,
  ).bind(eventId).first());
}

export async function firebaseGroupState(
  env: Env,
  fingerprint: string,
): Promise<FirebaseGroupState | null> {
  return env.DB.prepare(
    `SELECT fingerprint, sample_state, sample_epoch, epoch_first_event_id, reserved_bytes,
            last_seen, compacted_at, archived_at, archive_reason,
            lease_owner, lease_generation, lease_expires_at
     FROM firebase_crash_group_state WHERE fingerprint = ?1`,
  ).bind(fingerprint).first<FirebaseGroupState>();
}

export async function reserveFirebaseGroup(
  env: Env,
  fingerprint: string,
  lastSeen: string,
): Promise<"reserved" | "full"> {
  const result = await env.DB.prepare(
    `INSERT INTO firebase_crash_group_state (
       fingerprint, sample_state, sample_epoch, epoch_first_event_id,
       reserved_bytes, last_seen, compacted_at, archived_at, archive_reason
     )
     SELECT ?1, 'active', 1, '', ?2, ?3, '', '', ''
     WHERE (SELECT COALESCE(SUM(reserved_bytes), 0) FROM firebase_crash_group_state) + ?2 <= ?4
       AND (
         NOT EXISTS (SELECT 1 FROM groups WHERE fingerprint = ?1) OR
         EXISTS (SELECT 1 FROM firebase_crash_group_state WHERE fingerprint = ?1)
       )
     ON CONFLICT (fingerprint) DO UPDATE SET
       sample_state = 'active',
       sample_epoch = CASE
         WHEN firebase_crash_group_state.sample_state IN ('archiving', 'archived')
           THEN firebase_crash_group_state.sample_epoch + 1
         ELSE firebase_crash_group_state.sample_epoch
       END,
       epoch_first_event_id = CASE
         WHEN firebase_crash_group_state.sample_state IN ('archiving', 'archived') THEN ''
         ELSE firebase_crash_group_state.epoch_first_event_id
       END,
       reserved_bytes = ?2,
       last_seen = CASE WHEN firebase_crash_group_state.last_seen < ?3
         THEN ?3 ELSE firebase_crash_group_state.last_seen END,
       compacted_at = '', archived_at = '', archive_reason = ''
     WHERE firebase_crash_group_state.reserved_bytes >= ?2 OR
       (SELECT COALESCE(SUM(reserved_bytes), 0) FROM firebase_crash_group_state)
         - firebase_crash_group_state.reserved_bytes + ?2 <= ?4`,
  ).bind(fingerprint, FIREBASE_ACTIVE_RESERVATION_BYTES, lastSeen, FIREBASE_STORAGE_BUDGET_BYTES).run();
  return Number(result.meta?.changes ?? 0) > 0 ? "reserved" : "full";
}

export async function reclaimUnusedFirebaseReservation(env: Env, fingerprint: string): Promise<void> {
  await env.DB.prepare(
    `DELETE FROM firebase_crash_group_state
     WHERE fingerprint = ?1 AND lease_generation = 0
       AND NOT EXISTS (SELECT 1 FROM groups WHERE fingerprint = ?1)
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1)`,
  ).bind(fingerprint).run();
}

export async function acquireFirebaseGroupLease(
  env: Env,
  fingerprint: string,
  now = new Date(),
): Promise<FirebaseGroupLease | null> {
  const owner = crypto.randomUUID();
  const acquiredAt = now.toISOString();
  const expiresAt = new Date(now.getTime() + FIREBASE_GROUP_LEASE_MS).toISOString();
  const acquired = await env.DB.prepare(
    `UPDATE firebase_crash_group_state
     SET lease_owner = ?2, lease_generation = lease_generation + 1, lease_expires_at = ?3
     WHERE fingerprint = ?1 AND (
       lease_owner = '' OR datetime(lease_expires_at) <= datetime(?4)
     )
     RETURNING lease_generation`,
  ).bind(fingerprint, owner, expiresAt, acquiredAt).first<{ lease_generation: number }>();
  return acquired ? { owner, generation: Number(acquired.lease_generation) } : null;
}

export async function renewFirebaseGroupLease(
  env: Env,
  fingerprint: string,
  lease: FirebaseGroupLease,
  now = new Date(),
): Promise<boolean> {
  const current = now.toISOString();
  const expiresAt = new Date(now.getTime() + FIREBASE_GROUP_LEASE_MS).toISOString();
  const result = await env.DB.prepare(
    `UPDATE firebase_crash_group_state SET lease_expires_at = ?4
     WHERE fingerprint = ?1 AND lease_owner = ?2 AND lease_generation = ?3
       AND datetime(lease_expires_at) > datetime(?5)`,
  ).bind(fingerprint, lease.owner, lease.generation, expiresAt, current).run();
  return Number(result.meta?.changes ?? 0) > 0;
}

export async function releaseFirebaseGroupLease(
  env: Env,
  fingerprint: string,
  lease: FirebaseGroupLease,
): Promise<void> {
  await env.DB.prepare(
    `UPDATE firebase_crash_group_state SET lease_owner = '', lease_expires_at = ''
     WHERE fingerprint = ?1 AND lease_owner = ?2 AND lease_generation = ?3`,
  ).bind(fingerprint, lease.owner, lease.generation).run();
}

export async function markFirebaseDelivered(env: Env, eventId: string): Promise<void> {
  await env.DB.prepare("DELETE FROM firebase_crash_outbox WHERE event_id = ?1").bind(eventId).run();
}

export async function recordFirebaseRetry(
  env: Env,
  eventId: string,
  state: FirebaseOutboxRow["state"],
  attempts: number,
): Promise<void> {
  const delaySeconds = Math.min(24 * 60 * 60, 30 * 2 ** Math.min(attempts, 11));
  const now = new Date();
  const next = new Date(now.getTime() + delaySeconds * 1000).toISOString();
  await env.DB.prepare(
    `UPDATE firebase_crash_outbox
     SET state = ?2, attempts = attempts + 1, next_attempt_at = ?3, updated_at = ?4
     WHERE event_id = ?1`,
  ).bind(eventId, state, next, now.toISOString()).run();
}

export async function dueFirebaseCrashes(env: Env): Promise<FirebaseOutboxRow[]> {
  const result = await env.DB.prepare(
    `SELECT event_id, fingerprint, payload, state, attempts, next_attempt_at, created_at, updated_at
     FROM firebase_crash_outbox
     WHERE next_attempt_at <= ?1 AND (
       state IN ('queued', 'projected') OR
       (state = 'processing' AND datetime(updated_at) < datetime('now', '-10 minutes'))
     )
     ORDER BY created_at LIMIT ?2`,
  ).bind(new Date().toISOString(), FIREBASE_OUTBOX_BATCH).all<FirebaseOutboxRow>();
  return result.results;
}

export async function purgeFirebaseDeliveryState(env: Env): Promise<void> {
  await env.DB.batch([
    env.DB.prepare("DELETE FROM firebase_crash_outbox WHERE datetime(created_at) < datetime('now', '-30 days')"),
    env.DB.prepare("DELETE FROM firebase_crash_receipts WHERE datetime(projected_at) < datetime('now', '-90 days')"),
    env.DB.prepare(
      `UPDATE firebase_crash_group_state SET lease_owner = '', lease_expires_at = ''
       WHERE lease_owner != '' AND datetime(lease_expires_at) < datetime('now')`,
    ),
  ]);
}
