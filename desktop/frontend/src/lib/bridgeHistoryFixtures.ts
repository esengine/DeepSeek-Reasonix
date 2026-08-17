import type {
  HistoryContentRef,
  HistoryEntry,
  HistoryMessage,
  HistorySlice,
  HistorySliceRequest,
} from "./types";

export function mockHistorySlice(
  tabID: string,
  messages: HistoryMessage[],
  req: HistorySliceRequest,
  benchMock: boolean,
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
  const empty: HistorySlice = { entries: [], nextCursor: "", hasOlder: false, totalTurns: turn, startTurn: 0, endTurn: 0, stale: false, revision: 0 };
  if (before <= 0 || messages.length === 0) return empty;
  const turns = Math.max(1, Math.floor(req.turns || 12));
  const newestTurn = turnsOf[before - 1];
  const oldestTurn = newestTurn > 0 ? Math.max(newestTurn - turns + 1, 1) : 0;
  let lo = 0;
  if (oldestTurn > 1) {
    lo = before;
    for (let index = 0; index < before; index += 1) {
      if (turnsOf[index] >= oldestTurn) {
        lo = index;
        break;
      }
    }
  }
  const maxEntries = Math.max(1, Math.floor(req.entries || 120));
  if (before - lo > maxEntries) lo = before - maxEntries;
  const entries = messages.slice(lo, before).map((message, index) => {
    const entryId = `smock-${tabID}:r0:m${lo + index}:o0`;
    const content = message.content ?? "";
    const reasoning = message.reasoning ?? "";
    const lazyContent = benchMock && content.includes("ASYNC LAYOUT EXPANSION COMPLETE");
    const stormContent = benchMock && content.includes("BENCH STORM HYDRATION RESOLVED");
    const stormReasoning = benchMock && reasoning.includes("BENCH STORM HYDRATION RESOLVED");
    const refs: HistoryEntry["refs"] = [];
    if (lazyContent || stormContent) {
      refs.push({ entryId, field: "content", size: content.length, chunks: 1, revision: 0, revKnown: false, digest: "" });
    }
    if (stormReasoning) {
      refs.push({ entryId, field: "reasoning", size: reasoning.length, chunks: 1, revision: 0, revKnown: false, digest: "" });
    }
    return {
      entryId,
      turn: turnsOf[lo + index],
      order: lo + index,
      message: refs.length > 0 ? {
        ...message,
        ...(lazyContent || stormContent ? { content: content.slice(0, 4 * 1024) } : {}),
        ...(stormReasoning ? { reasoning: reasoning.slice(0, 4 * 1024) } : {}),
      } : message,
      refs,
    };
  });
  const visibleTurns = entries.map((entry) => entry.turn).filter((value) => value > 0);
  return {
    entries,
    nextCursor: lo > 0 ? btoa(JSON.stringify({ v: 1, before: lo })) : "",
    hasOlder: lo > 0,
    totalTurns: turn,
    startTurn: visibleTurns.length > 0 ? Math.min(...visibleTurns) : 0,
    endTurn: visibleTurns.length > 0 ? Math.max(...visibleTurns) : 0,
    stale: false,
    revision: 0,
  };
}

export function mockHistoryContentField(message: HistoryMessage, ref: HistoryContentRef): string {
  switch (ref.field) {
    case "content": return message.content ?? "";
    case "reasoning": return message.reasoning ?? "";
    case "submitText": return message.submitText ?? "";
    case "detail": return message.detail ?? "";
    case "code": return message.code ?? "";
    case "summary": return message.summary ?? "";
    case "archive": return message.archive ?? "";
    case "toolResultError": return message.toolResultError ?? "";
    case "toolArguments": return (message.toolCalls ?? []).find((toolCall) => toolCall.id === ref.toolCallId)?.arguments ?? "";
    case "toolSubject": return (message.toolCalls ?? []).find((toolCall) => toolCall.id === ref.toolCallId)?.subject ?? "";
    case "toolSummary": return (message.toolCalls ?? []).find((toolCall) => toolCall.id === ref.toolCallId)?.summary ?? "";
    case "toolDiff": return (message.toolCalls ?? []).find((toolCall) => toolCall.id === ref.toolCallId)?.diff ?? "";
    default: return "";
  }
}
