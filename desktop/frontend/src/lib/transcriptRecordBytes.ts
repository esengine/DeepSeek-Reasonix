import type { HistoryMessage } from "./types";

// recordBytes approximates the retained UTF-16 size of a record's inline text
// (the same fields the Go side counts for its slice byte budget).
export function recordBytes(m: HistoryMessage): number {
  let chars =
    (m.content?.length ?? 0) +
    (m.reasoning?.length ?? 0) +
    (m.submitText?.length ?? 0) +
    (m.detail?.length ?? 0) +
    (m.code?.length ?? 0) +
    (m.summary?.length ?? 0) +
    (m.archive?.length ?? 0) +
    (m.toolResultError?.length ?? 0) +
    (m.toolCallId?.length ?? 0) +
    (m.toolName?.length ?? 0) +
    (m.role?.length ?? 0);
  if (m.completionReceipt) chars += JSON.stringify(m.completionReceipt).length;
  if (m.completionSummary) chars += JSON.stringify(m.completionSummary).length;
  for (const tc of m.toolCalls ?? []) {
    chars +=
      (tc.arguments?.length ?? 0) +
      (tc.subject?.length ?? 0) +
      (tc.summary?.length ?? 0) +
      (tc.diff?.length ?? 0) +
      (tc.id?.length ?? 0) +
      (tc.name?.length ?? 0);
  }
  return chars * 2;
}
