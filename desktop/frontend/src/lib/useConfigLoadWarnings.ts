import { useCallback, useEffect, useRef, useState } from "react";

export interface ConfigLoadWarningsSnapshot {
  warnings: string[];
  revision: number;
}

export function normalizeConfigLoadWarnings(payload: unknown): string[] {
  if (!Array.isArray(payload)) return [];
  const seen = new Set<string>();
  const warnings: string[] = [];
  for (const value of payload) {
    if (typeof value !== "string") continue;
    const warning = value.trim();
    if (!warning || seen.has(warning)) continue;
    seen.add(warning);
    warnings.push(warning);
  }
  return warnings;
}

export function configLoadWarningsKey(warnings: readonly string[]): string {
  return warnings.length > 0 ? JSON.stringify(warnings) : "";
}

export function normalizeConfigLoadWarningsRevision(value: unknown): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;
}

export function normalizeConfigLoadWarningsEvent(payload: unknown, revisionValue?: unknown): ConfigLoadWarningsSnapshot | null {
  if (Array.isArray(payload)) {
    const warnings = normalizeConfigLoadWarnings(payload);
    return warnings.length > 0 ? { warnings, revision: normalizeConfigLoadWarningsRevision(revisionValue) } : null;
  }
  if (!payload || typeof payload !== "object") return null;
  const record = payload as Record<string, unknown>;
  const warnings = normalizeConfigLoadWarnings(record.warnings);
  if (warnings.length === 0) return null;
  return { warnings, revision: normalizeConfigLoadWarningsRevision(record.revision) };
}

export function subscribeConfigLoadWarnings(cb: (snapshot: ConfigLoadWarningsSnapshot) => void): () => void {
  if (typeof window !== "undefined" && window.go?.main?.App && window.runtime) {
    return window.runtime.EventsOn("config:load-warnings", (payload?: unknown, revision?: unknown) => {
      const snapshot = normalizeConfigLoadWarningsEvent(payload, revision);
      if (snapshot) cb(snapshot);
    });
  }
  return () => {};
}

export function useConfigLoadWarnings() {
  const [configLoadWarnings, setConfigLoadWarnings] = useState<string[]>([]);
  const latestRevision = useRef(0);
  const seenKeys = useRef(new Set<string>());
  const present = useCallback((payload: unknown, revisionValue: unknown, resetSeen = false) => {
    const revision = normalizeConfigLoadWarningsRevision(revisionValue);
    if (revision < latestRevision.current) return;
    latestRevision.current = revision;
    const warnings = normalizeConfigLoadWarnings(payload);
    if (resetSeen) seenKeys.current.clear();
    const key = configLoadWarningsKey(warnings);
    if (!key) {
      seenKeys.current.clear();
      setConfigLoadWarnings([]);
      return;
    }
    if (seenKeys.current.has(key)) return;
    seenKeys.current.add(key);
    setConfigLoadWarnings(warnings);
  }, []);

  useEffect(() => subscribeConfigLoadWarnings((snapshot) => {
    present(snapshot.warnings, snapshot.revision);
  }), [present]);

  const applySnapshot = useCallback((payload: unknown, revision: unknown) => {
    present(payload, revision);
  }, [present]);
  const reload = useCallback((payload: unknown, revision: unknown) => present(payload, revision, true), [present]);
  const dismiss = useCallback(() => setConfigLoadWarnings([]), []);

  return { configLoadWarnings, applySnapshot, reload, dismiss };
}
