import { useState } from "react";
import type { TranscriptRow } from "./transcriptRows";

export const TRANSCRIPT_VIRTUOSO_INDEX_BASE = 1_000_000;

export type TranscriptVirtuosoIndexState = {
  resetKey: string;
  keys: readonly string[];
  firstItemIndex: number;
};

function sameKeys(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((key, index) => key === right[index]);
}

export function reconcileTranscriptVirtuosoIndex(
  previous: TranscriptVirtuosoIndexState,
  rows: readonly TranscriptRow[],
  resetKey: string,
): TranscriptVirtuosoIndexState {
  const keys = rows.map((row) => String(row.key));
  if (previous.resetKey !== resetKey) {
    return { resetKey, keys, firstItemIndex: TRANSCRIPT_VIRTUOSO_INDEX_BASE };
  }
  if (sameKeys(previous.keys, keys)) return previous;

  // Locate any stable overlap near the old start. A positive delta means rows
  // were prepended; feeding the inverse delta to firstItemIndex is Virtuoso's
  // contract for keeping the previous viewport in place.
  let delta = 0;
  const searchLimit = Math.min(previous.keys.length, 128);
  for (let oldIndex = 0; oldIndex < searchLimit; oldIndex += 1) {
    const newIndex = keys.indexOf(previous.keys[oldIndex]);
    if (newIndex < 0) continue;
    delta = newIndex - oldIndex;
    break;
  }
  // If no overlap was found in the search window, treat the entire key set as
  // replaced. The safe fallback is to keep firstItemIndex unchanged so the
  // viewport stays where it was, rather than jumping arbitrarily.
  if (delta === 0 && searchLimit > 0 && keys.length > 0 && !keys.includes(previous.keys[0])) {
    // No overlap: full replacement. Maintain the previous index anchor.
    return { resetKey, keys, firstItemIndex: previous.firstItemIndex };
  }
  return {
    resetKey,
    keys,
    firstItemIndex: Math.max(0, previous.firstItemIndex - delta),
  };
}

export function useTranscriptVirtuosoFirstItemIndex(rows: readonly TranscriptRow[], resetKey: string): number {
  const [state, setState] = useState<TranscriptVirtuosoIndexState>(() => ({
    resetKey,
    keys: rows.map((row) => String(row.key)),
    firstItemIndex: TRANSCRIPT_VIRTUOSO_INDEX_BASE,
  }));
  const next = reconcileTranscriptVirtuosoIndex(state, rows, resetKey);
  if (next === state) return state.firstItemIndex;
  setState(next);
  return next.firstItemIndex;
}
