import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { loadWorkspaceGitStats } from "./workspaceGitStats";
import { createSerialWorkspacePoll } from "./serialWorkspacePoll";

export interface DiffStats {
  added: number;
  removed: number;
  incomplete: boolean;
}

export function useWorkspaceDiffStats(tabId: string, scopeKey: string, workspaceRoot: string, enabled: boolean) {
  const [snapshot, setSnapshot] = useState<{ key: string; stats: DiffStats } | null>(null);
  const key = JSON.stringify([tabId, scopeKey, workspaceRoot]);
  const pollRef = useRef<ReturnType<typeof createSerialWorkspacePoll> | null>(null);
  if (!pollRef.current) pollRef.current = createSerialWorkspacePoll({
    schedule: (callback, delay) => window.setTimeout(callback, delay),
    cancel: (handle) => window.clearTimeout(handle as number),
  });

  useLayoutEffect(() => {
    const poll = pollRef.current!;
    let generation = 0;
    const reconcile = () => {
      const request = ++generation;
      if (!enabled || !tabId || document.visibilityState === "hidden") {
        poll.setJob(null);
        return;
      }
      poll.setJob(async () => {
        try {
          const result = await loadWorkspaceGitStats(tabId, workspaceRoot, () => request === generation);
          if (request !== generation) return;
          setSnapshot({ key, stats: {
            added: result?.added ?? 0, removed: result?.removed ?? 0,
            incomplete: result?.incomplete === true || result?.gitAvailable !== true,
          } });
        } catch {
          if (request !== generation) return;
          setSnapshot(current => ({ key, stats: {
            added: current?.key === key ? current.stats.added : 0,
            removed: current?.key === key ? current.stats.removed : 0, incomplete: true,
          } }));
        }
      });
    };
    reconcile();
    document.addEventListener("visibilitychange", reconcile);
    return () => {
      generation++;
      poll.setJob(null);
      document.removeEventListener("visibilitychange", reconcile);
    };
  }, [enabled, tabId, workspaceRoot, key]);

  const reloadDiffStats = useCallback(() => pollRef.current?.refresh(), []);
  return { diffStats: snapshot?.key === key ? snapshot.stats : null, reloadDiffStats };
}
