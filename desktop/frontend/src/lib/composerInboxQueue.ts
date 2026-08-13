import type { PendingGuidance } from "../components/ComposerGuidanceShelf";

export type InboxSnapshotLike = {
  paused?: boolean;
  recovered?: boolean;
  recoveredCount?: number;
  sessionPath?: string;
  items?: Array<{
    id: string;
    preview?: string;
    state?: string;
    intent?: string;
    source?: string;
  }>;
};

export function inboxScopeKey(sessionPath?: string, workspaceScopeKey?: string): string {
  return (sessionPath || "").trim() || workspaceScopeKey || "";
}

export function inboxSnapshotBelongsToScope(snapshotPath: string | undefined, scopeKey: string): boolean {
  const path = (snapshotPath || "").trim();
  if (!path || !scopeKey) return true;
  if (scopeKey === path) return true;
  return scopeKey.split("\u0000").includes(path);
}

export function localGuidanceFallback(previewKey: string): PendingGuidance[] {
  return previewKey
    .split("\n")
    .filter(Boolean)
    .map((text, i) => ({ id: `local-${i}`, text, submitText: text }));
}

export function guidanceFromInboxSnapshot(snap: InboxSnapshotLike | null | undefined): PendingGuidance[] {
  return (snap?.items ?? []).map((it) => ({
    id: it.id,
    text: it.preview || "",
    submitText: "",
    state: it.state,
    intent: it.intent,
    source: it.source,
    paused: Boolean(snap?.paused),
    recoveredCount: snap?.paused && snap?.recovered
      ? (snap.recoveredCount || snap.items?.length || 0)
      : undefined,
  }));
}

export async function hydrateEmptyGuidancePreviews(
  items: PendingGuidance[],
  readItem: (id: string) => Promise<{ displayText?: string; submitText?: string; rawText?: string }>,
): Promise<PendingGuidance[]> {
  const missing = items.filter((item) => !item.text.trim() && !item.id.startsWith("local-"));
  if (missing.length === 0) return items;
  await Promise.all(missing.map(async (item) => {
    try {
      const env = await readItem(item.id);
      const text = (env.displayText || env.submitText || env.rawText || "").trim();
      if (text) item.text = text;
    } catch {
      // Preview-only snapshot remains if the body cannot be read.
    }
  }));
  return items;
}

export function mergeGuidanceSnapshot(durable: PendingGuidance[], fallback: PendingGuidance[]): PendingGuidance[] {
  return durable.length > 0 ? durable : fallback;
}
