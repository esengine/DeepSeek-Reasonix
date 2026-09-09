import { useLayoutEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { BackgroundRuntimeView, WorkspaceConflictView } from "../lib/types";
import { createPollingOwner, type PollClock } from "./pollingOwner";
import { trackAppOperation } from "./appLifecycleProbe";

const browserClock: PollClock = {
  setTimeout: (callback, delay) => window.setTimeout(callback, delay),
  clearTimeout: handle => window.clearTimeout(handle as number),
};
export function useRuntimeStatus(input: { tabId?: string; sessionKey: string; running: boolean }, clock: PollClock = browserClock) {
  const [backgroundRuntimes, setBackgroundRuntimes] = useState<BackgroundRuntimeView[]>([]);
  const [conflict, setConflict] = useState<{ key: string; value: WorkspaceConflictView | null } | null>(null);
  const background = useRef<ReturnType<typeof createPollingOwner<BackgroundRuntimeView[]>> | null>(null);
  const refreshBackgroundRuntimes = useCommittedCommand(() => background.current?.refresh() ?? Promise.resolve());
  useLayoutEffect(() => {
    const owner = createPollingOwner({ target: { kind: "application" }, periodMs: 1000, clock,
      read: app.BackgroundRuntimes, publish: setBackgroundRuntimes, failed: () => {},
    }, trackAppOperation);
    background.current = owner;
    void owner.refresh();
    return () => { owner.dispose(); if (background.current === owner) background.current = null; };
  }, [clock]);
  const { tabId, sessionKey, running } = input;
  const key = JSON.stringify([tabId, sessionKey]);
  const setWorkspaceConflict = useCommittedCommand((value: WorkspaceConflictView | null) => setConflict(value ? { key, value } : null));
  useLayoutEffect(() => {
    setConflict(null);
    if (!tabId || !running) return;
    const owner = createPollingOwner({ target: { kind: "session", tabId, sessionKey }, periodMs: 500, clock,
      read: () => app.WorkspaceConflictForTab(tabId),
      publish: value => setConflict({ key, value: value.state === "none" ? null : value }),
      failed: () => setConflict({ key, value: null }),
    }, trackAppOperation);
    void owner.refresh();
    return () => owner.dispose();
  }, [clock, key, running, sessionKey, tabId]);
  return { backgroundRuntimes, refreshBackgroundRuntimes, setWorkspaceConflict, workspaceConflict: conflict?.key === key ? conflict.value : null };
}
