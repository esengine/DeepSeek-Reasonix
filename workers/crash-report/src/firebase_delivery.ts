import type { Env } from "./env";
import { Report, type ReportPayload } from "./report_schema";
import {
  acquireFirebaseGroupLease,
  claimFirebaseCrash,
  crashStorageMode,
  dueFirebaseCrashes,
  firebaseGroupState,
  firebaseOutboxExists,
  firebaseProjectionReceipt,
  firebaseStorageReady,
  markFirebaseDelivered,
  recordFirebaseRetry,
  releaseFirebaseGroupLease,
  renewFirebaseGroupLease,
  type FirebaseGroupLease,
  type FirebaseOutboxRow,
} from "./crash_delivery";
import { firebaseMeta, loadFirebaseGroupMeta } from "./firebase_crash_view";
import { writeFirebaseCrashGroup } from "./firebase_rtdb";

const LATEST_SAMPLES_PER_GROUP = 5;

export type StoredCrashEvent = {
  eventId: string;
  fingerprint: string;
  receivedAt: string;
  keepD1Sample: boolean;
  report: ReportPayload;
};

export async function deliverCrashEventToFirebase(
  env: Env,
  event: StoredCrashEvent,
  attempts: number,
  lease: FirebaseGroupLease,
): Promise<boolean> {
  try {
    const [group, receipt, state] = await Promise.all([
      loadFirebaseGroupMeta(env, event.fingerprint),
      firebaseProjectionReceipt(env, event.eventId),
      firebaseGroupState(env, event.fingerprint),
    ]);
    if (!group || !receipt || !state || state.sample_state !== "active") {
      throw new Error("firebase projection state is missing");
    }
    const { installId: _installId, ...publicReport } = event.report;
    const stillLatest = Number(receipt.group_count) > Number(group.count) - LATEST_SAMPLES_PER_GROUP;
    const renew = () => renewFirebaseGroupLease(env, event.fingerprint, lease);
    await writeFirebaseCrashGroup(
      env,
      firebaseMeta(group),
      { ...publicReport, eventId: event.eventId, receivedAt: event.receivedAt },
      stillLatest ? Number(receipt.latest_slot) : null,
      Number(receipt.first_sample) === 1,
      lease.generation,
      Number(state.sample_epoch),
      renew,
    );
    if (!await renew()) throw new Error("firebase crash group lease was fenced before delivery completion");
    await markFirebaseDelivered(env, event.eventId);
    return true;
  } catch (error) {
    console.error("firebase crash delivery failed", error);
    await recordFirebaseRetry(env, event.eventId, "projected", attempts);
    return false;
  }
}

function parseStoredCrash(row: FirebaseOutboxRow): StoredCrashEvent | undefined {
  try {
    const value = JSON.parse(row.payload) as Partial<StoredCrashEvent>;
    const report = Report.safeParse(value.report);
    if (
      !report.success || value.eventId !== row.event_id || value.fingerprint !== row.fingerprint ||
      !/^(?:dev:)?[0-9a-f]{64}$/.test(value.fingerprint ?? "") ||
      typeof value.receivedAt !== "string" || typeof value.keepD1Sample !== "boolean"
    ) return undefined;
    return { ...value, report: report.data } as StoredCrashEvent;
  } catch {
    return undefined;
  }
}

export async function drainFirebaseCrashOutbox(
  env: Env,
  projectCrashEvent: (env: Env, event: StoredCrashEvent) => Promise<void>,
): Promise<void> {
  if (crashStorageMode(env) === "d1" || !firebaseStorageReady(env)) return;
  for (const row of await dueFirebaseCrashes(env)) {
    const event = parseStoredCrash(row);
    if (!event) {
      console.error(`firebase crash outbox row ${row.event_id} is invalid`);
      await recordFirebaseRetry(env, row.event_id, "queued", row.attempts);
      continue;
    }
    const lease = await acquireFirebaseGroupLease(env, row.fingerprint);
    if (!lease) continue;
    try {
      if (!await firebaseOutboxExists(env, row.event_id)) continue;
      if (row.state !== "projected") {
        if (!await claimFirebaseCrash(env, row.event_id, new Date().toISOString())) continue;
        try {
          await projectCrashEvent(env, event);
        } catch (error) {
          console.error("firebase crash projection failed", error);
          await recordFirebaseRetry(env, row.event_id, "queued", row.attempts);
          continue;
        }
      }
      if (!await deliverCrashEventToFirebase(env, event, row.attempts, lease)) break;
    } finally {
      await releaseFirebaseGroupLease(env, row.fingerprint, lease).catch((error) => {
        console.error("firebase crash group lease release failed", error);
      });
    }
  }
}
