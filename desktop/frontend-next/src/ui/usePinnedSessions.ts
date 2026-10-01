import { useCallback, useEffect, useRef, useState } from "react";
import type { HubPort, TreeWorkspace } from "../port/hub";

const KEY = "reasonix:pinned-sessions";

function saved(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

// The kernel owns pins, because its automatic archiving has to honour them.
// Once the tree has loaded the window sends the union of its own memory and
// what the kernel reported, and adopts that union; a failed send is retried on
// the next tree load, never assumed done. Until it lands the kernel archives
// nothing on its own.
export function usePinnedSessions(hub: HubPort, tree: TreeWorkspace[], treeRead: boolean) {
  const [pinned, setPinned] = useState<Set<string>>(saved);
  const synced = useRef(false);

  const set = useCallback((path: string, on: boolean) => {
    setPinned((current) => {
      if (current.has(path) === on) return current;
      const next = new Set(current);
      if (on) next.add(path);
      else next.delete(path);
      try {
        localStorage.setItem(KEY, JSON.stringify([...next]));
      } catch {
        // the kernel still has it
      }
      return next;
    });
  }, []);

  const toggle = useCallback(
    (path: string) => {
      const on = !pinned.has(path);
      set(path, on);
      void hub.pinSession(path, on).catch(() => {});
    },
    [hub, pinned, set],
  );

  const unpin = useCallback((path: string) => set(path, false), [set]);

  useEffect(() => {
    if (!treeRead || synced.current) return;
    const union = new Set(pinned);
    for (const ws of tree) for (const s of ws.sessions) if (s.pinned) union.add(s.path);
    hub
      .syncPins([...union])
      .then(() => {
        synced.current = true;
        for (const path of union) set(path, true);
      })
      .catch(() => {});
  }, [hub, tree, treeRead, pinned, set]);

  return [pinned, toggle, unpin] as const;
}
