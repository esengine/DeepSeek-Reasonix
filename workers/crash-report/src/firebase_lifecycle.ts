import type { Env } from "./env";
import {
  FIREBASE_ARCHIVING_RESERVATION_BYTES,
  FIREBASE_ACTIVE_RESERVATION_BYTES,
  FIREBASE_COMPACTED_RESERVATION_BYTES,
  FIREBASE_OUTBOX_LIMIT,
  FIREBASE_STORAGE_BUDGET_BYTES,
  acquireFirebaseGroupLease,
  firebaseGroupState,
  releaseFirebaseGroupLease,
  renewFirebaseGroupLease,
  type FirebaseGroupLease,
  type FirebaseSampleState,
} from "./crash_delivery";
import { firebaseMeta, loadFirebaseGroupMeta } from "./firebase_crash_view";
import {
  deleteFirebaseCrashGroupConditional,
  writeFirebaseGroupMeta,
  writeFirebaseSampleMarkers,
} from "./firebase_rtdb";

const LIFECYCLE_BATCH = 20;

export type FirebaseStorageSummary = {
  active: number;
  compacted: number;
  archiving: number;
  archived: number;
  reservedBytes: number;
  budgetBytes: number;
  outboxCount: number;
  oldestOutboxSeconds: number;
  stuckArchiving: number;
};

type LifecycleCandidate = {
  fingerprint: string;
  sample_state: FirebaseSampleState;
};

function renewer(env: Env, fingerprint: string, lease: FirebaseGroupLease) {
  return () => renewFirebaseGroupLease(env, fingerprint, lease);
}

async function pendingOutbox(env: Env, fingerprint: string): Promise<boolean> {
  return Boolean(await env.DB.prepare(
    "SELECT event_id FROM firebase_crash_outbox WHERE fingerprint = ?1 LIMIT 1",
  ).bind(fingerprint).first());
}

async function compactGroup(env: Env, fingerprint: string, lease: FirebaseGroupLease, now: string): Promise<void> {
  const [group, state] = await Promise.all([
    loadFirebaseGroupMeta(env, fingerprint), firebaseGroupState(env, fingerprint),
  ]);
  if (!group || !state || state.sample_state !== "active" || await pendingOutbox(env, fingerprint)) return;
  const renew = renewer(env, fingerprint, lease);
  await writeFirebaseSampleMarkers(
    env, fingerprint, Number(group.count), lease.generation, Number(state.sample_epoch),
    "compacted", false, renew,
  );
  await writeFirebaseGroupMeta(
    env, fingerprint, firebaseMeta(group), lease.generation, Number(state.sample_epoch),
    "compacted", renew,
  );
  await env.DB.prepare(
    `UPDATE firebase_crash_group_state
     SET sample_state = 'compacted', reserved_bytes = ?4, compacted_at = ?5
     WHERE fingerprint = ?1 AND sample_state = 'active'
       AND lease_owner = ?2 AND lease_generation = ?3
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1)`,
  ).bind(fingerprint, lease.owner, lease.generation, FIREBASE_COMPACTED_RESERVATION_BYTES, now).run();
}

async function beginArchive(
  env: Env,
  fingerprint: string,
  lease: FirebaseGroupLease,
  now: string,
  reason: "retention" | "admin",
): Promise<void> {
  const [group, state] = await Promise.all([
    loadFirebaseGroupMeta(env, fingerprint), firebaseGroupState(env, fingerprint),
  ]);
  if (!group || !state || !["active", "compacted"].includes(state.sample_state)) {
    if (reason === "admin") throw new Error("firebase crash group lifecycle state is missing");
    return;
  }
  if (reason === "retention" && await pendingOutbox(env, fingerprint)) return;
  const renew = renewer(env, fingerprint, lease);
  await writeFirebaseSampleMarkers(
    env, fingerprint, Number(group.count), lease.generation, Number(state.sample_epoch),
    "archiving", true, renew,
  );
  await writeFirebaseGroupMeta(
    env, fingerprint, firebaseMeta(group), lease.generation, Number(state.sample_epoch),
    "archiving", renew,
  );
  if (reason === "admin") {
    await env.DB.batch([
      env.DB.prepare("DELETE FROM reports WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare("DELETE FROM report_daily WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare("DELETE FROM report_installations WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare("DELETE FROM report_event_dimensions WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare(
        "DELETE FROM firebase_crash_outbox WHERE fingerprint = ?1 AND datetime(created_at) <= datetime(?2)",
      ).bind(fingerprint, now),
      env.DB.prepare("DELETE FROM groups WHERE fingerprint = ?1").bind(fingerprint),
      env.DB.prepare(
        `UPDATE firebase_crash_group_state
         SET sample_state = CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN 'active' ELSE 'archiving' END,
             sample_epoch = sample_epoch + CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN 1 ELSE 0 END,
             epoch_first_event_id = CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN '' ELSE epoch_first_event_id END,
             reserved_bytes = CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN ?4 ELSE ?5 END,
             archived_at = CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN '' ELSE ?6 END,
             archive_reason = CASE WHEN EXISTS (
               SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1
             ) THEN '' ELSE 'admin' END
         WHERE fingerprint = ?1 AND lease_owner = ?2 AND lease_generation = ?3`,
      ).bind(
        fingerprint, lease.owner, lease.generation,
        FIREBASE_ACTIVE_RESERVATION_BYTES, FIREBASE_ARCHIVING_RESERVATION_BYTES, now,
      ),
    ]);
    return;
  }
  await env.DB.prepare(
    `UPDATE firebase_crash_group_state
     SET sample_state = 'archiving', reserved_bytes = ?4, archived_at = ?5, archive_reason = 'retention'
     WHERE fingerprint = ?1 AND sample_state IN ('active', 'compacted')
       AND lease_owner = ?2 AND lease_generation = ?3
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1)`,
  ).bind(fingerprint, lease.owner, lease.generation, FIREBASE_ARCHIVING_RESERVATION_BYTES, now).run();
}

async function finishArchive(env: Env, fingerprint: string, lease: FirebaseGroupLease): Promise<void> {
  const state = await firebaseGroupState(env, fingerprint);
  if (!state || state.sample_state !== "archiving" || await pendingOutbox(env, fingerprint)) return;
  await deleteFirebaseCrashGroupConditional(env, fingerprint, renewer(env, fingerprint, lease));
  if (state.archive_reason === "admin") {
    await env.DB.prepare(
      `DELETE FROM firebase_crash_group_state
       WHERE fingerprint = ?1 AND sample_state = 'archiving'
         AND lease_owner = ?2 AND lease_generation = ?3
         AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1)`,
    ).bind(fingerprint, lease.owner, lease.generation).run();
  } else {
    await env.DB.prepare(
      `UPDATE firebase_crash_group_state
       SET sample_state = 'archived', reserved_bytes = 0, lease_owner = '', lease_expires_at = ''
       WHERE fingerprint = ?1 AND sample_state = 'archiving'
         AND lease_owner = ?2 AND lease_generation = ?3
         AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = ?1)`,
    ).bind(fingerprint, lease.owner, lease.generation).run();
  }
}

async function withLease(
  env: Env,
  candidate: LifecycleCandidate,
  operation: (lease: FirebaseGroupLease) => Promise<void>,
): Promise<boolean> {
  const lease = await acquireFirebaseGroupLease(env, candidate.fingerprint);
  if (!lease) return false;
  try {
    await operation(lease);
    return true;
  } finally {
    await releaseFirebaseGroupLease(env, candidate.fingerprint, lease);
  }
}

export async function archiveFirebaseGroupForAdmin(env: Env, fingerprint: string): Promise<void> {
  const lease = await acquireFirebaseGroupLease(env, fingerprint);
  if (!lease) throw new Error("firebase crash group is busy");
  try {
    await beginArchive(env, fingerprint, lease, new Date().toISOString(), "admin");
  } finally {
    await releaseFirebaseGroupLease(env, fingerprint, lease);
  }
}

export async function runFirebaseCrashLifecycle(env: Env, now = new Date()): Promise<void> {
  const iso = now.toISOString();
  let remaining = LIFECYCLE_BATCH;
  const queries = [
    `SELECT fingerprint, sample_state FROM firebase_crash_group_state
     WHERE sample_state = 'archiving' AND datetime(archived_at) <= datetime(?1, '-24 hours')
     ORDER BY archived_at LIMIT ?2`,
    `SELECT state.fingerprint, state.sample_state FROM firebase_crash_group_state AS state
     JOIN groups USING (fingerprint)
     WHERE state.sample_state IN ('active', 'compacted') AND groups.status IN ('resolved', 'ignored')
       AND datetime(groups.last_seen) <= datetime(?1, '-60 days')
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = state.fingerprint)
     ORDER BY groups.last_seen LIMIT ?2`,
    `SELECT state.fingerprint, state.sample_state FROM firebase_crash_group_state AS state
     JOIN groups USING (fingerprint)
     WHERE state.sample_state = 'active' AND groups.status IN ('resolved', 'ignored')
       AND datetime(groups.last_seen) <= datetime(?1, '-30 days')
       AND NOT EXISTS (SELECT 1 FROM firebase_crash_outbox WHERE fingerprint = state.fingerprint)
     ORDER BY groups.last_seen LIMIT ?2`,
  ];
  for (let phase = 0; phase < queries.length && remaining > 0; phase++) {
    const candidates = await env.DB.prepare(queries[phase]).bind(iso, remaining).all<LifecycleCandidate>();
    for (const candidate of candidates.results) {
      const processed = await withLease(env, candidate, async (lease) => {
        if (phase === 0) await finishArchive(env, candidate.fingerprint, lease);
        else if (phase === 1) await beginArchive(env, candidate.fingerprint, lease, iso, "retention");
        else await compactGroup(env, candidate.fingerprint, lease, iso);
      });
      if (processed) remaining--;
      if (remaining === 0) break;
    }
  }
}

export async function firebaseStorageSummary(env: Env): Promise<FirebaseStorageSummary> {
  const [state, outbox] = await Promise.all([
    env.DB.prepare(
      `SELECT
         SUM(CASE WHEN sample_state = 'active' THEN 1 ELSE 0 END) AS active,
         SUM(CASE WHEN sample_state = 'compacted' THEN 1 ELSE 0 END) AS compacted,
         SUM(CASE WHEN sample_state = 'archiving' THEN 1 ELSE 0 END) AS archiving,
         SUM(CASE WHEN sample_state = 'archived' THEN 1 ELSE 0 END) AS archived,
         COALESCE(SUM(reserved_bytes), 0) AS reserved_bytes,
         SUM(CASE WHEN sample_state = 'archiving' AND datetime(archived_at) < datetime('now', '-48 hours')
           THEN 1 ELSE 0 END) AS stuck_archiving
       FROM firebase_crash_group_state`,
    ).first<Record<string, number>>(),
    env.DB.prepare(
      `SELECT COUNT(*) AS outbox_count,
              COALESCE(CAST((julianday('now') - julianday(MIN(created_at))) * 86400 AS INTEGER), 0)
                AS oldest_outbox_seconds
       FROM firebase_crash_outbox`,
    ).first<Record<string, number>>(),
  ]);
  return {
    active: Number(state?.active ?? 0),
    compacted: Number(state?.compacted ?? 0),
    archiving: Number(state?.archiving ?? 0),
    archived: Number(state?.archived ?? 0),
    reservedBytes: Number(state?.reserved_bytes ?? 0),
    budgetBytes: FIREBASE_STORAGE_BUDGET_BYTES,
    outboxCount: Number(outbox?.outbox_count ?? 0),
    oldestOutboxSeconds: Number(outbox?.oldest_outbox_seconds ?? 0),
    stuckArchiving: Number(state?.stuck_archiving ?? 0),
  };
}

export const FIREBASE_OUTBOX_WARNING = Math.floor(FIREBASE_OUTBOX_LIMIT * 0.8);
