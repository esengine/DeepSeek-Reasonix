import { createContext, useRef, useSyncExternalStore } from "react";
import type { Item } from "./session_types";
import { foldModes, onFoldModesChange, type Fold, type FoldMode, type FoldModes } from "./prefs";

export function useFoldModes(): FoldModes {
  return useSyncExternalStore(onFoldModesChange, foldModes, foldModes);
}

/** Whether a part starts open under its mode, given whether it is still being
 *  written, whether it failed and whether it changed a file. */
export function startsOpen(mode: FoldMode, live = false, failed = false, changed = false): boolean {
  return mode === "open" || (mode === "live" && live) || (mode === "failed" && failed) || (mode === "changed" && changed);
}

/** How a block of this kind starts, recomputed as the part's state changes. A
 *  block the reader has opened or closed keeps that answer instead. */
export function useStartsOpen(kind: Fold, live = false, failed = false, changed = false): boolean {
  return startsOpen(useFoldModes()[kind], live, failed, changed);
}

const NONE: ReadonlySet<string> = new Set();

/** Ids of the work items in the turn that is still running, which is what a
 *  "live" activity group reads: open for the whole turn, not only while one of
 *  its calls runs, so the pauses between calls do not fold it. */
export const LiveWork = createContext<ReadonlySet<string>>(NONE);

export function liveWork(items: readonly Item[], running: boolean): ReadonlySet<string> {
  if (!running) return NONE;
  const ids = new Set<string>();
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t === "user" && !it.pending) break;
    if (it.t === "tool" || it.t === "reads") ids.add(it.id);
  }
  return ids;
}

/** liveWork held by identity while its members stay the same, so a streamed
 *  token does not hand every activity group a new set to re-render on. */
export function useLiveWork(items: readonly Item[], running: boolean): ReadonlySet<string> {
  const held = useRef<ReadonlySet<string>>(NONE);
  const next = liveWork(items, running);
  if (next.size !== held.current.size || [...next].some((id) => !held.current.has(id))) held.current = next;
  return held.current;
}
