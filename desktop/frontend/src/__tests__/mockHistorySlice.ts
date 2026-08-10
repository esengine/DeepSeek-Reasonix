// Shared HistorySliceForTab mock helper for controller tests: windows a full
// HistoryMessage[] the same way the bridge dev mock does (turn-budgeted
// suffix pages, opaque base64 cursor, stable mock entry ids). Tests that mock
// window.go.main.App build their HistorySliceForTab from their HistoryForTab
// with this.
//
//   HistorySliceForTab: async (tabID, req) =>
//     historySliceFromMessages(tabID, await window.go.main.App.HistoryForTab(tabID), req),

import type { HistorySlice, HistorySliceRequest } from "../lib/types";
import type { HistoryMessage } from "../lib/types";

export function historySliceFromMessages(
  tabID: string,
  messages: HistoryMessage[],
  req: HistorySliceRequest,
  identity: { revision?: number; digest?: string } = {},
): HistorySlice {
  const turnsOf: number[] = [];
  let turn = 0;
  for (const message of messages) {
    if (message.role === "user") turn += 1;
    turnsOf.push(turn);
  }
  let before = messages.length;
  if (req.cursor) {
    try {
      const decoded = JSON.parse(atob(req.cursor)) as { before?: number };
      if (typeof decoded.before === "number" && decoded.before >= 0 && decoded.before < before) before = decoded.before;
    } catch { /* unknown cursor: serve the latest page */ }
  }
  const revision = identity.revision ?? 0;
  const digest = identity.digest ?? "";
  const empty: HistorySlice = { entries: [], nextCursor: "", hasOlder: false, totalTurns: turn, startTurn: 0, endTurn: 0, stale: false, revision, revisionKnown: revision > 0, digest };
  if (before <= 0 || messages.length === 0) return empty;
  const turns = Math.max(1, Math.floor(req.turns || 12));
  const newestTurn = turnsOf[before - 1];
  const oldestTurn = newestTurn > 0 ? Math.max(newestTurn - turns + 1, 1) : 0;
  let lo = 0;
  if (oldestTurn > 1) {
    lo = before;
    for (let i = 0; i < before; i += 1) {
      if (turnsOf[i] >= oldestTurn) { lo = i; break; }
    }
  }
  const entries = messages.slice(lo, before).map((message, index) => ({
    entryId: `smock-${tabID}:r0:m${lo + index}:o0`,
    turn: turnsOf[lo + index],
    order: lo + index,
    message,
    refs: [],
  }));
  const visibleTurns = entries.map((entry) => entry.turn).filter((value) => value > 0);
  return {
    entries,
    nextCursor: lo > 0 ? btoa(JSON.stringify({ v: 1, before: lo })) : "",
    hasOlder: lo > 0,
    totalTurns: turn,
    startTurn: visibleTurns.length > 0 ? Math.min(...visibleTurns) : 0,
    endTurn: visibleTurns.length > 0 ? Math.max(...visibleTurns) : 0,
    stale: false,
    revision,
    revisionKnown: revision > 0,
    digest,
  };
}
