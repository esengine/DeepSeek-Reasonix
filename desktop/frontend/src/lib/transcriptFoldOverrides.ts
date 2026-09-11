import type { FoldEntry, FoldMap, FoldSegmentState } from "./transcriptRows";

const MAX_SESSIONS = 12;
const MAX_SEGMENTS_PER_SESSION = 512;
const overrides = new Map<string, Map<string, FoldEntry>>();

function sessionMap(sessionKey: string): Map<string, FoldEntry> {
  const existing = overrides.get(sessionKey);
  if (existing) {
    overrides.delete(sessionKey);
    overrides.set(sessionKey, existing);
    return existing;
  }
  const created = new Map<string, FoldEntry>();
  overrides.set(sessionKey, created);
  while (overrides.size > MAX_SESSIONS) overrides.delete(overrides.keys().next().value!);
  return created;
}

export function readTranscriptFoldOverrides(
  sessionKey: string,
  segments: readonly FoldSegmentState[],
): FoldMap {
  const stored = sessionMap(sessionKey);
  const visible = new Set(segments.map((segment) => segment.key));
  return new Map(Array.from(stored).filter(([key]) => visible.has(key)));
}

export function writeTranscriptFoldOverride(sessionKey: string, segmentKey: string, entry: FoldEntry): void {
  const stored = sessionMap(sessionKey);
  stored.delete(segmentKey);
  stored.set(segmentKey, entry);
  while (stored.size > MAX_SEGMENTS_PER_SESSION) stored.delete(stored.keys().next().value!);
}

export function replaceTranscriptFoldOverrides(sessionKey: string, folds: FoldMap): void {
  const stored = sessionMap(sessionKey);
  for (const [key, entry] of folds) {
    if (!entry.userOverridden) continue;
    stored.delete(key);
    stored.set(key, entry);
  }
  while (stored.size > MAX_SEGMENTS_PER_SESSION) stored.delete(stored.keys().next().value!);
}

export function clearTranscriptFoldOverrideSessionForTest(sessionKey: string): void {
  overrides.delete(sessionKey);
}
