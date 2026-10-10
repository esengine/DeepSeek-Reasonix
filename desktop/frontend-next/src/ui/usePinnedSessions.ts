import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { HubPort, TreeWorkspace } from "../port/hub";

const KEY = "reasonix:pinned-sessions";

function legacy(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

function keep(paths: Set<string>) {
  try {
    if (paths.size === 0) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, JSON.stringify([...paths]));
  } catch {
    // the pins come back from the kernel on the next load
  }
}

// The kernel owns pins because its automatic archiving has to honour them, so
// what is drawn is what the tree reports. localStorage only carries pins made
// before the kernel kept them: each is sent once, when its conversation is
// first listed, and forgotten. A toggle shows at once and is dropped as soon
// as the tree agrees; a refused one is withdrawn and reported.
export function usePinnedSessions(hub: HubPort, tree: TreeWorkspace[], treeRead: boolean, onFailure: (e: unknown) => void = () => {}) {
  const [shown, setShown] = useState<Map<string, boolean>>(new Map());
  const old = useRef<Set<string> | null>(null);
  const opened = useRef(false);

  const kernel = useMemo(() => {
    const out = new Set<string>();
    for (const ws of tree) for (const s of ws.sessions) if (s.pinned) out.add(s.path);
    return out;
  }, [tree]);

  const pinned = useMemo(() => {
    const out = new Set(kernel);
    for (const [path, on] of shown) {
      if (on) out.add(path);
      else out.delete(path);
    }
    return out;
  }, [kernel, shown]);

  useEffect(() => {
    setShown((cur) => {
      const next = new Map([...cur].filter(([path, on]) => kernel.has(path) !== on));
      return next.size === cur.size ? cur : next;
    });
  }, [kernel]);

  const show = useCallback((path: string, on: boolean | null) => {
    setShown((cur) => {
      const next = new Map(cur);
      if (on === null) next.delete(path);
      else next.set(path, on);
      return next;
    });
  }, []);

  const toggle = useCallback(
    (path: string) => {
      const on = !pinned.has(path);
      show(path, on);
      hub.pinSession(path, on).catch((e) => {
        show(path, null);
        onFailure(e);
      });
    },
    [hub, onFailure, pinned, show],
  );

  const unpin = useCallback((path: string) => show(path, false), [show]);

  useEffect(() => {
    if (!treeRead) return;
    old.current ??= legacy();
    const listed = new Set<string>();
    for (const ws of tree) for (const s of ws.sessions) listed.add(s.path);
    const move = [...old.current].filter((path) => listed.has(path));
    const send = move.filter((path) => !kernel.has(path));
    if (opened.current && send.length === 0) {
      if (move.length > 0) {
        for (const path of move) old.current.delete(path);
        keep(old.current);
      }
      return;
    }
    hub
      .syncPins(send)
      .then(() => {
        opened.current = true;
        for (const path of move) old.current?.delete(path);
        if (old.current) keep(old.current);
        for (const path of send) show(path, true);
      })
      .catch(() => {});
  }, [hub, tree, treeRead, kernel, show]);

  return [pinned, toggle, unpin] as const;
}
