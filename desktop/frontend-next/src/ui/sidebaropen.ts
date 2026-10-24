import { useCallback } from "react";
import type { Dispatch, SetStateAction } from "react";

export interface SidebarOpenRequest {
  root?: string;
  sessionPath?: string;
}

export interface SidebarRuntime {
  id: string;
  sessionPath?: string;
}

/** Picking a conversation that is already on screen brings it back into view
 *  rather than re-opening it: the pane may be sitting on its analysis or
 *  workbench page, and the click is the way out of that page.
 *
 *  It lives here rather than in `App` because it is a rule about panes, not a
 *  wiring detail of the window — and because `App` is already at its size
 *  ceiling. */
export function useSidebarOpen<V extends string>(
  runtimes: SidebarRuntime[],
  openPane: (req: SidebarOpenRequest) => Promise<unknown>,
  setActive: (id: string) => void,
  setViewRequests: Dispatch<SetStateAction<Record<string, { to: V; n: number }>>>,
  reading: V,
) {
  return useCallback(
    (req: SidebarOpenRequest): Promise<void> => {
      const held = req.sessionPath ? runtimes.find((rt) => rt.sessionPath === req.sessionPath) : undefined;
      if (!held) {
        // The caller awaits this to know an open failed; `void` here would
        // swallow the rejection and leave the row stuck on its old selection.
        return openPane(req).then(() => undefined);
      }
      setActive(held.id);
      setViewRequests((requests) => ({
        ...requests,
        [held.id]: { to: reading, n: (requests[held.id]?.n ?? 0) + 1 },
      }));
      return Promise.resolve();
    },
    [runtimes, openPane, setActive, setViewRequests, reading],
  );
}
