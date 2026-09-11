import type { Item, State } from "./useController";
import type { HistoryMessage, WireCompletionSummary } from "./types";
import { t } from "./i18n";
import { completionSummaryPresentation, normalizeCompletionSummary, sessionQualityFloor } from "./completionSummary";
import { mergeTurnResult } from "./turnResult";

function lastUserIndex(items: Item[]): number {
  for (let i = items.length - 1; i >= 0; i--) if (items[i].kind === "user") return i;
  return -1;
}

export function historicalResultNotice(message: HistoryMessage, id: string): Extract<Item, { kind: "notice" }> | undefined {
  const summary = normalizeCompletionSummary(mergeTurnResult(message.completionSummary, message.completionReceipt, message.turnId, message.checkpointTurn));
  const presentation = completionSummaryPresentation(summary, "standard", t);
  return presentation ? { kind: "notice", id, variant: "completion", action: "open_changes", level: presentation.level, title: presentation.title, text: presentation.body, completionSummary: summary } : undefined;
}

/** Replace only the current user turn's result, keeping its mounted identity. */
export function withTurnResult(s: State, summary: WireCompletionSummary): State {
  const boundary = lastUserIndex(s.items);
  const index = s.items.findIndex((item, i) => i > boundary && item.kind === "notice" && item.variant === "completion");
  const presentation = completionSummaryPresentation(summary, sessionQualityFloor(s.meta), t);
  if (!presentation) return { ...s, completionSummary: summary, items: index < 0 ? s.items : s.items.filter((_, i) => i !== index) };
  const id = index < 0 ? `q${s.seq}` : s.items[index].id;
  const notice: Item = { kind: "notice", id, variant: "completion", action: "open_changes", level: presentation.level, title: presentation.title, text: presentation.body, completionSummary: summary };
  const items = [...s.items];
  if (index < 0) items.push(notice); else items[index] = notice;
  return { ...s, completionSummary: summary, items, seq: s.seq + Number(index < 0) };
}

export function withRunningChecks(s: State): State {
  const boundary = lastUserIndex(s.items);
  const tools = s.items.slice(boundary + 1).filter((item): item is Extract<Item, { kind: "tool" }> => item.kind === "tool" && item.verifying === true && item.status === "running");
  if (tools.length === 0 && !s.completionSummary?.checking) return s;
  const liveChecks = tools.map(tool => {
    let command = tool.name;
    try { const args = JSON.parse(tool.args); command = typeof args.command === "string" ? args.command : command; } catch { /* incomplete legacy arguments */ }
    return { toolCallId: tool.id, command, output: tool.output?.slice(-65536) };
  });
  const summary = mergeTurnResult(s.completionSummary, undefined, s.activeTurnId);
  return withTurnResult(s, { ...summary, checking: tools.length > 0, liveChecks });
}
