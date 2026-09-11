import { app } from "./bridge";
import type { WorkspaceChangesView } from "./types";

// A launcher can unmount while its RPC is still running. Hold the scope until
// completion so a replacement launcher cannot overlap that scan or reuse it.
const inflight = new Map<string, Promise<WorkspaceChangesView>>();

export async function loadWorkspaceGitStats(tabId: string, workspaceRoot: string, current: () => boolean) {
  const key = JSON.stringify([tabId, workspaceRoot]);
  while (inflight.has(key)) {
    try { await inflight.get(key); } catch { /* The replacement owns its own result. */ }
    if (!current()) return undefined;
  }
  if (!current()) return undefined;
  const request = app.WorkspaceGitStatsForTab(tabId, workspaceRoot);
  inflight.set(key, request);
  try {
    return await request;
  } finally {
    if (inflight.get(key) === request) inflight.delete(key);
  }
}
