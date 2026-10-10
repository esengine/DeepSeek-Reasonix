import { useCallback, useMemo, useState } from "react";
import type { HostCapabilities, HubPort, TreeWorkspace } from "../port/hub";

export interface Adder {
  add: () => void;
  busy: boolean;
  /** True while the headless fallback is asking for a server-side path. */
  pathOpen: boolean;
  addPath: (path: string, onAdded?: (workspace: TreeWorkspace) => Promise<void>) => void;
  closePath: () => void;
}

const LEGACY_CAPABILITIES: HostCapabilities = { pickFolder: true, addWorkspace: true };

/** useAddWorkspace is the one implementation of "open a project", so the
 *  sidebar's entry and the first-run banner cannot drift into two behaviours. */
export function useAddWorkspace(hub: HubPort, reload: () => Promise<void>, onError: (e: unknown) => void): Adder {
  const [busy, setBusy] = useState(false);
  const [pathOpen, setPathOpen] = useState(false);

  const add = useCallback(
    () => {
      if (busy) return;
      setBusy(true);
      void hub
        .hostCapabilities()
        .catch(() => LEGACY_CAPABILITIES)
        .then(async (capabilities) => {
          if (!capabilities.addWorkspace) {
            throw new Error("此内核不支持添加工作区。");
          }
          if (!capabilities.pickFolder) {
            setPathOpen(true);
            return;
          }
          const dir = await hub.pickFolder();
          if (dir === null) {
            setPathOpen(true);
            return;
          }
          // "" is the user closing the panel — an answer, not a reason to ask again.
          if (!dir) return;
          await hub.addWorkspace(dir);
          await reload();
        })
        .catch(onError)
        .finally(() => setBusy(false));
    },
    [busy, hub, reload, onError],
  );

  const addPath = useCallback(
    (raw: string, onAdded?: (workspace: TreeWorkspace) => Promise<void>) => {
      const path = raw.trim();
      if (busy || !path) return;
      setBusy(true);
      void hub
        .addWorkspace(path)
        .then(async (workspace) => {
          await reload();
          await onAdded?.(workspace);
        })
        .then(() => setPathOpen(false))
        .catch((e) => {
          // The error bar sits above the app; an overlay would hide the reason.
          setPathOpen(false);
          onError(e);
        })
        .finally(() => setBusy(false));
    },
    [busy, hub, reload, onError],
  );

  const closePath = useCallback(() => {
    if (!busy) setPathOpen(false);
  }, [busy]);

  return useMemo(() => ({ add, busy, pathOpen, addPath, closePath }), [add, busy, pathOpen, addPath, closePath]);
}
