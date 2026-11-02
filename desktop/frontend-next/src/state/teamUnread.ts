import { useSyncExternalStore } from "react";

// Which agents and groups moved since the person last looked, kept on this
// machine only: the kernel says when something last moved, and when it was
// last looked at is this window's own fact. A mark the person set by hand
// stays until they open the thing.
const SEEN = "rx-team-seen";
const MARKED = "rx-team-unread";

interface UnreadState {
  seen: Record<string, number>;
  marked: string[];
}

const listeners = new Set<() => void>();
let cachedKey: string | null = null;
let cached: UnreadState = { seen: {}, marked: [] };

function parse<T>(raw: string | null, fallback: T): T {
  try {
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

function snapshot(): UnreadState {
  const seen = localStorage.getItem(SEEN);
  const marked = localStorage.getItem(MARKED);
  const key = `${seen}\u0000${marked}`;
  if (key !== cachedKey) {
    cachedKey = key;
    cached = { seen: parse(seen, {}), marked: parse(marked, []) };
  }
  return cached;
}

function write(next: UnreadState) {
  localStorage.setItem(SEEN, JSON.stringify(next.seen));
  localStorage.setItem(MARKED, JSON.stringify(next.marked));
  for (const l of listeners) l();
}

export function useTeamUnread(): UnreadState {
  return useSyncExternalStore((l) => {
    listeners.add(l);
    return () => void listeners.delete(l);
  }, snapshot);
}

export function isUnread(s: UnreadState, id: string, at: number | undefined): boolean {
  if (s.marked.includes(id)) return true;
  const seen = s.seen[id];
  return seen !== undefined && (at ?? 0) > seen;
}

/** Records what the person has seen. Something met for the first time is
 *  taken as read: only what moves after that is news. */
export function noteSeen(entries: { id: string; at?: number; looking?: boolean }[]) {
  const s = snapshot();
  const seen = { ...s.seen };
  let marked = s.marked;
  let changed = false;
  for (const e of entries) {
    const at = e.at ?? 0;
    if (seen[e.id] === undefined || (e.looking && seen[e.id] !== at)) {
      seen[e.id] = at;
      changed = true;
    }
    if (e.looking && marked.includes(e.id)) {
      marked = marked.filter((m) => m !== e.id);
      changed = true;
    }
  }
  if (changed) write({ seen, marked });
}

export function markUnread(id: string) {
  const s = snapshot();
  if (!s.marked.includes(id)) write({ seen: s.seen, marked: [...s.marked, id] });
}
