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
// The window tells it every pin it remembers that the kernel does not list as
// pinned, on each tree load until that lands, so a legacy pin in a workspace
// added later is protected before its first sweep. A pin the kernel refused is
// rolled back and reported rather than left looking kept.
export function usePinnedSessions(hub: HubPort, tree: TreeWorkspace[], treeRead: boolean, onFailure: (e: unknown) => void = () => {}) {
  const [pinned, setPinned] = useState<Set<string>>(saved);

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
      hub.pinSession(path, on).catch((e) => {
        set(path, !on);
        onFailure(e);
      });
    },
    [hub, onFailure, pinned, set],
  );

  const unpin = useCallback((path: string) => set(path, false), [set]);

  const synced = useRef(false);
  useEffect(() => {
    if (!treeRead) return;
    const kernel = new Set<string>();
    const listed = new Set<string>();
    for (const ws of tree) {
      for (const s of ws.sessions) {
        listed.add(s.path);
        if (s.pinned) kernel.add(s.path);
      }
    }
    const unknown = [...pinned].filter((path) => listed.has(path) && !kernel.has(path));
    if (synced.current && unknown.length === 0) return;
    const union = new Set([...unknown, ...kernel]);
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
