import type { ProjectNode } from "./types";
import { projectTreeReadActivityKey, topicReadRevision, topicUsesResultSequence, type ProjectTreeReadActivity } from "./projectTreeTopic";

export type ReadRecord = {
  metric: "result" | "time";
  value: number;
  revision: number;
  imported?: boolean;
  verifiedVersion?: string;
  repairVersion?: number;
  readFloor?: number;
  needsBaseline?: boolean;
  migratedFromTime?: boolean;
};
export type ReadStore = { version: 3; baselineAt: number; records: Record<string, ReadRecord> };
export type BaselineObservation = { complete: boolean; resultSequence: number; eventVersion: string };

export function readActivityValues(store: ReadStore, nodes: readonly ProjectNode[] = []): ProjectTreeReadActivity {
  const values = Object.fromEntries(Object.entries(store.records).map(([key, record]) => [key, record.needsBaseline ? Number.MAX_SAFE_INTEGER : record.value]));
  const visit = (node: ProjectNode) => {
    const key = projectTreeReadActivityKey(node);
    // A downgrade/cached old row cannot compare a timestamp with a result count.
    if (key && store.records[key]?.metric === "result" && !topicUsesResultSequence(node)) values[key] = Number.MAX_SAFE_INTEGER;
    node.children?.forEach(visit);
  };
  nodes.forEach(visit);
  return values;
}

export function markSessionRead(store: ReadStore, node: ProjectNode, now = Date.now()): ReadStore {
  const key = projectTreeReadActivityKey(node);
  if (!key) return store;
  const metric = topicUsesResultSequence(node) ? "result" : "time";
  if (node.remoteSession && metric === "result" && node.turnsState !== "ready") return store;
  const value = metric === "result" ? topicReadRevision(node) : Math.max(topicReadRevision(node), now);
  const previous = store.records[key];
  if (previous?.metric === "result" && metric === "time") return store;
  if (value < 0 || (value === 0 && (metric === "time" || node.turnsState !== "ready")) || (previous?.metric === metric && previous.value >= value && !previous.needsBaseline && (!previous.imported || (previous.readFloor ?? 0) >= value))) return store;
  return { ...store, records: { ...store.records, [key]: { metric,
    value: previous?.metric === metric ? Math.max(previous.value, value) : value,
    revision: (previous?.revision ?? 0) + 1, repairVersion: previous?.repairVersion,
    imported: previous?.metric === metric ? previous.imported : undefined,
    migratedFromTime: previous?.migratedFromTime || previous?.metric === "time" && metric === "result",
    readFloor: previous?.metric === metric && previous.imported ? Math.max(previous.readFloor ?? 0,value) : undefined } } };
}

export function repairReadBaseline(store: ReadStore, key: string, captured: ReadRecord, observation: BaselineObservation): ReadStore {
  const current = store.records[key];
  if (current?.needsBaseline && current.value === captured.value && current.revision === captured.revision
    && observation.complete && observation.resultSequence > 0 && observation.eventVersion) {
    return { ...store, records: { ...store.records, [key]: { ...current, metric: "result", value: observation.resultSequence,
      revision: current.revision + 1, needsBaseline: false, verifiedVersion: observation.eventVersion } } };
  }
  if (!current || !current.imported || current.metric !== "result" || current.repairVersion
    || current.value !== captured.value || current.revision !== captured.revision
    || !observation.complete || observation.resultSequence <= 0 || observation.resultSequence < (current.readFloor ?? 0) || !observation.eventVersion) return store;
  return { ...store, records: { ...store.records, [key]: { ...current,
    value: Math.min(current.value, observation.resultSequence), imported: false,
    revision: current.revision + 1, repairVersion: 1, verifiedVersion: observation.eventVersion } } };
}

/** Normal writes merge monotonically. A one-time repair is accepted only for
 * the exact imported record revision it verified, never over a user's read. */
export function mergeReadStores(current: ReadStore, incoming: ReadStore): ReadStore {
  let records = current.records;
  for (const [key, next] of Object.entries(incoming.records)) {
    const old = records[key];
    if (old && !old.needsBaseline && next.needsBaseline) continue;
    if (old?.repairVersion && next.imported) {
      if (next.metric === old.metric && (next.readFloor ?? 0) > old.value) {
        if (records === current.records) records = { ...records };
        records[key] = { ...old, value: next.readFloor!, readFloor: next.readFloor, revision: Math.max(old.revision, next.revision) + 1 };
      }
      continue;
    }
    const metricMigration = old?.metric === "time" && next.metric === "result" && next.migratedFromTime;
    if (old?.metric !== undefined && old.metric !== next.metric && !metricMigration) continue;
    const repair = old?.imported && !old.repairVersion && next.repairVersion === 1
      && next.verifiedVersion && next.revision === old.revision + 1;
    if (!old || metricMigration || repair || next.value > old.value
      || next.value === old.value && next.revision > old.revision) {
      if (records === current.records) records = { ...records };
      records[key] = next;
    }
  }
  return records === current.records ? current : { ...current, records };
}

export function seedSessionReads(store: ReadStore, nodes: readonly ProjectNode[]): ReadStore {
  let next = store;
  const visit = (node: ProjectNode) => {
    const key = projectTreeReadActivityKey(node);
    const previous = key ? next.records[key] : undefined;
    if (key && topicUsesResultSequence(node) && node.turnsState === "ready"
      && (!previous || node.remoteSession && previous.metric === "time")) {
      // Source time values must never become canonical result sequences.
      const needsBaseline = !node.remoteSession && (node.identityAliases?.some(alias => next.records[alias]?.metric === "time") ?? false);
      next = { ...next, records: { ...next.records, [key]: { metric: "result", value: needsBaseline ? 0 : topicReadRevision(node),
        revision: (previous?.revision ?? 0) + 1, needsBaseline, migratedFromTime: previous?.metric === "time" } } };
    }
    node.children?.forEach(visit);
  };
  nodes.forEach(visit);
  return next;
}
