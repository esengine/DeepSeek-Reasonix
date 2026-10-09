import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AgentPort } from "../port/port";
import type { RuntimeView, TreeWorkspace } from "../port/hub";

type Trees = Record<string, TreeWorkspace[] | null>;

const keyOf = (host: string | undefined, path: string) => `${host ?? ""}\n${path}`;

// The kernel's answer with the rows the person has just opened drawn as seen.
// Same reference back when nothing is cleared, so a memoised sidebar stays put.
export function seen(tree: TreeWorkspace[], host: string, cleared: Set<string>): TreeWorkspace[] {
  if (cleared.size === 0) return tree;
  let changed = false;
  const next = tree.map((ws) => {
    if (!ws.sessions.some((s) => s.unread && cleared.has(keyOf(host, s.path)))) return ws;
    changed = true;
    return { ...ws, sessions: ws.sessions.map((s) => (s.unread && cleared.has(keyOf(host, s.path)) ? { ...s, unread: false } : s)) };
  });
  return changed ? next : tree;
}

export function isUnread(rt: RuntimeView, tree: TreeWorkspace[], remote: Trees): boolean {
  if (!rt.sessionPath) return false;
  const book = rt.host ? remote[rt.host] ?? [] : tree;
  return book.some((ws) => ws.sessions.some((s) => s.path === rt.sessionPath && !!s.unread));
}

interface Inputs {
  runtimes: RuntimeView[];
  ports: Map<string, AgentPort>;
  active: string;
  tree: TreeWorkspace[];
  remote: Trees;
  reload: () => Promise<unknown>;
}

// Viewing is the window's decision and the kernel never infers it: a session
// becoming the pane in front, or a turn ending in the pane already there while
// the window has focus. A refused call puts the mark back rather than saying so.
export function useViewed({ runtimes, ports, active, tree, remote, reload }: Inputs) {
  const [cleared, setCleared] = useState<Set<string>>(() => new Set());
  const now = useRef({ runtimes, ports, tree, remote, reload, active });
  now.current = { runtimes, ports, tree, remote, reload, active };
  const inflight = useRef(new Set<string>());

  const again = useRef(new Set<string>());
  const held = useRef(new Set<string>());

  const mark = useCallback((id: string, owed: boolean) => {
    const cur = now.current;
    const rt = cur.runtimes.find((r) => r.id === id);
    const port = cur.ports.get(id);
    if (!rt?.sessionPath || !port) return;
    const key = keyOf(rt.host, rt.sessionPath);
    if (inflight.current.has(key)) {
      if (owed) again.current.add(key);
      return;
    }
    if (!owed && !isUnread(rt, cur.tree, cur.remote)) return;
    inflight.current.add(key);
    const set = (on: boolean) =>
      setCleared((prev) => {
        if (prev.has(key) === on) return prev;
        const next = new Set(prev);
        if (on) next.add(key);
        else next.delete(key);
        return next;
      });
    set(true);
    port
      .markSessionViewed()
      .then(() => now.current.reload().then(() => true, () => false))
      .catch(() => null)
      .then((read) => {
        inflight.current.delete(key);
        if (read === false) held.current.add(key);
        else set(false);
        const owes = again.current.delete(key);
        const rt = now.current.runtimes.find((r) => r.id === id);
        if (owes && rt?.sessionPath && keyOf(rt.host, rt.sessionPath) === key) mark(id, true);
      });
  }, []);

  const view = useCallback((id: string) => mark(id, false), [mark]);
  const turnDone = useCallback(
    (id: string) => {
      if (id === now.current.active && document.hasFocus()) mark(id, true);
    },
    [mark],
  );

  const front = useRef(active);
  useEffect(() => {
    if (front.current === active || !ports.has(active)) return;
    front.current = active;
    view(active);
  }, [active, ports, view]);

  useEffect(() => {
    if (held.current.size === 0) return;
    const drop = held.current;
    held.current = new Set();
    setCleared((prev) => {
      const next = new Set([...prev].filter((k) => !drop.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [tree, remote]);

  const local = useMemo(() => seen(tree, "", cleared), [tree, cleared]);
  const far = useMemo(() => {
    const out: Trees = {};
    for (const [host, book] of Object.entries(remote)) out[host] = book ? seen(book, host, cleared) : book;
    return out;
  }, [remote, cleared]);

  return { view, turnDone, tree: local, remote: far };
}
