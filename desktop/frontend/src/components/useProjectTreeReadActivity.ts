import { useCallback, useEffect, useMemo, useState } from "react";
import { app } from "../lib/bridge";
import type { ProjectNode } from "../lib/types";
import { projectTreeReadActivityKey } from "../lib/projectTreeTopic";
import { markSessionRead, mergeReadStores, readActivityValues, repairReadBaseline, seedSessionReads, type ReadRecord, type ReadStore } from "../lib/sessionReadActivity";

const STORE_KEY = "projectTree:readActivity:v3";

function loadStore(): ReadStore {
  const empty: ReadStore = { version: 3, baselineAt: Date.now(), records: {} };
  try {
    const stored = localStorage.getItem(STORE_KEY);
    if (stored) {
      const data = JSON.parse(stored) as ReadStore;
      if (data.version === 3 && data.records) return data;
    }
    const legacy = JSON.parse(localStorage.getItem("projectTree:readActivity") || "{}") as Record<string, unknown>;
    const baseline = Number(localStorage.getItem("projectTree:readActivityBaselineAt"));
    if (baseline > 0) empty.baselineAt = baseline;
    for (const [key, value] of Object.entries(legacy)) {
      if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) continue;
      // Topic timestamps are retained solely as compatibility import data;
      // source rows use their own keys and the existing time baseline.
      const canonical = key.startsWith("session\u001f");
      const parts = key.split("\u001f");
      const target = canonical ? `ref\u0000${parts[1] || "local"}\u0000${parts[2]}` : key;
      empty.records[target] = { metric: canonical ? "result" : "time", value, revision: 0, imported: true };
    }
    localStorage.setItem(STORE_KEY, JSON.stringify(empty));
  } catch { /* Storage may be unavailable. */ }
  return empty;
}

function persist(store: ReadStore): ReadStore {
  try {
    const merged = mergeReadStores(loadStore(), store);
    localStorage.setItem(STORE_KEY, JSON.stringify(merged));
    return merged;
  } catch { return store; }
}

// Deduplicate verification across mounted trees; Query also bounds cold rebuilds.
const verifications = new Map<string, ReturnType<typeof app.GetSessionActivityBaseline>>();
let activeVerifications = 0;
const waitingVerifications: (() => void)[] = [];
function verifyBaseline(node: ProjectNode, key: string) {
  const existing = verifications.get(key);
  if (existing) return existing;
  const request = new Promise<Awaited<ReturnType<typeof app.GetSessionActivityBaseline>>>((resolve, reject) => {
    const run = () => {
      activeVerifications++;
      void app.GetSessionActivityBaseline({ ref: node.session }).then(resolve, reject).finally(() => {
        activeVerifications--;
        waitingVerifications.shift()?.();
      });
    };
    if (activeVerifications < 2) run(); else waitingVerifications.push(run);
  });
  verifications.set(key, request);
  void request.finally(() => verifications.delete(key)).catch(() => {});
  return request;
}

export function useProjectTreeReadActivity(nodes: readonly ProjectNode[]) {
  const [store, setStore] = useState<ReadStore>(loadStore);
  const [verificationEpoch, setVerificationEpoch] = useState(0);
  const readActivity = useMemo(() => readActivityValues(store, nodes), [store, nodes]);
  const markNodeRead = useCallback((node: ProjectNode) => {
    setStore(current => {
      const next = markSessionRead(current, node);
      return next === current ? current : persist(next);
    });
  }, []);

  useEffect(() => {
    setStore(current => {
      const next = seedSessionReads(current, nodes);
      return next === current ? current : persist(next);
    });
  }, [nodes]);

  useEffect(() => {
    let alive = true;
    let retry: ReturnType<typeof setTimeout> | undefined;
    const retryLater = () => {
      if (alive && !retry) retry = setTimeout(() => { if (alive) setVerificationEpoch(value => value + 1); }, 10_000);
    };
    const candidates: { node: ProjectNode; key: string; record: ReadRecord }[] = [];
    const visit = (node: ProjectNode) => {
      const key = projectTreeReadActivityKey(node), record = key ? store.records[key] : undefined;
      if (node.session && (!node.session.hostId || node.session.hostId === "local") && key && record?.metric === "result"
        && (record.needsBaseline || record.imported && !record.repairVersion)) candidates.push({ node, key, record });
      node.children?.forEach(visit);
    };
    nodes.forEach(visit);
    for (const { node, key, record } of candidates) {
      const request = verifyBaseline(node, key);
      void request.then(observation => {
        if (!alive || observation.ref.hostId !== node.session?.hostId || observation.ref.sessionId !== node.session?.sessionId) return;
        if (!observation.complete || observation.resultSequence === 0) retryLater();
        setStore(current => {
          const next = repairReadBaseline(current, key, record, observation);
          return next === current ? current : persist(next);
        });
      }).catch(retryLater);
    }
    return () => { alive = false; if (retry) clearTimeout(retry); };
  }, [nodes, store, verificationEpoch]);

  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key !== STORE_KEY || !event.newValue) return;
      try {
        const incoming = JSON.parse(event.newValue) as ReadStore;
        if (incoming.version === 3 && incoming.records) setStore(current => mergeReadStores(current, incoming));
      } catch { /* Ignore invalid external storage. */ }
    };
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, []);
  return { readActivity, readBaselineAt: store.baselineAt, markNodeRead };
}
