import type { Env } from "./env";
import type { ReportSample } from "./group";
import type {
  FirebaseCrashGroupMeta,
  FirebaseCrashSample,
} from "./firebase_rtdb";

export type FirebaseGroupRow = {
  fingerprint: string;
  kind: string;
  count: number;
  first_seen: string;
  last_seen: string;
  first_version: string;
  last_version: string;
  status: string;
  title: string;
  source: string;
  label: string;
  error_type: string;
  top_frame: string;
  severity: string;
  last_os: string;
  last_arch: string;
  last_build_commit: string;
  last_channel: string;
  regressed_at: string;
};

export async function loadFirebaseGroupMeta(
  env: Env,
  fingerprint: string,
): Promise<FirebaseGroupRow | null> {
  return env.DB.prepare(
    `SELECT fingerprint, kind, count, first_seen, last_seen, first_version, last_version,
            status, title, source, label, error_type, top_frame, severity, last_os, last_arch,
            last_build_commit, last_channel, regressed_at
     FROM groups WHERE fingerprint = ?1`,
  ).bind(fingerprint).first<FirebaseGroupRow>();
}

export function firebaseMeta(group: FirebaseGroupRow): FirebaseCrashGroupMeta {
  return {
    fingerprint: group.fingerprint,
    kind: group.kind,
    count: Number(group.count),
    firstSeen: group.first_seen,
    lastSeen: group.last_seen,
    firstVersion: group.first_version,
    lastVersion: group.last_version,
    status: group.status,
    title: group.title,
    source: group.source,
    label: group.label,
    errorType: group.error_type,
    topFrame: group.top_frame,
    severity: group.severity,
    lastOS: group.last_os,
    lastArch: group.last_arch,
    lastBuildCommit: group.last_build_commit,
    lastChannel: group.last_channel,
    regressedAt: group.regressed_at,
    writerGeneration: 0,
    sampleEpoch: 1,
    sampleState: "active",
  };
}

export function firebaseSamples(
  samples?: {
    first?: FirebaseCrashSample;
    latest?: Record<string, FirebaseCrashSample | { marker?: unknown }> |
      Array<FirebaseCrashSample | { marker?: unknown }>;
  },
): ReportSample[] {
  if (!samples) return [];
  const isSample = (value: FirebaseCrashSample | { marker?: unknown }): value is FirebaseCrashSample =>
    typeof (value as Partial<FirebaseCrashSample>).eventId === "string";
  const latest = Array.isArray(samples.latest)
    ? samples.latest.filter(isSample)
    : Object.values(samples.latest ?? {}).filter(isSample);
  const unique = new Map<string, FirebaseCrashSample>();
  for (const sample of [...latest, ...(samples.first ? [samples.first] : [])]) {
    if (sample && typeof sample.eventId === "string") unique.set(sample.eventId, sample);
  }
  return [...unique.values()]
    .sort((left, right) => String(right.receivedAt).localeCompare(String(left.receivedAt)))
    .map(firebaseSampleToReport);
}

function firebaseSampleToReport(sample: FirebaseCrashSample): ReportSample {
  const string = (key: string) => typeof sample[key] === "string" ? sample[key] as string : "";
  const json = (key: string, fallback: unknown) => JSON.stringify(sample[key] ?? fallback);
  return {
    version: string("version"),
    os: string("os"),
    arch: string("arch"),
    message: string("message"),
    device: json("device", {}),
    created_at: string("receivedAt"),
    source: string("source"),
    label: string("label"),
    error_type: string("errorType"),
    error_message: string("errorMessage"),
    top_frame: string("topFrame"),
    build_commit: string("buildCommit"),
    channel: string("channel"),
    language: string("language"),
    view: string("view"),
    breadcrumbs: json("breadcrumbs", []),
    component_stack: string("componentStack"),
    stack: string("stack"),
    occurred_at: string("occurredAt"),
    webview2: sample.webview2 ? json("webview2", {}) : "",
    web_runtime: sample.webRuntime ? json("webRuntime", {}) : "",
  };
}
