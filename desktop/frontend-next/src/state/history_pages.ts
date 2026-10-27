import type { HistoryToolCall } from "../port/session";
import type { Item, SessionState } from "./session_types";
import type { Executions } from "./executions";
import { foldHistoryItems } from "./fold";

export const historyToolId = (snapshot: string, message: number, ordinal: number) => `history:${snapshot}:${message}:tool:${ordinal}`;

export function historyToolItem(call: HistoryToolCall, id: string, stable: boolean): Item {
  return { t: "tool", id, tool: { id: call.id, name: call.name, args: call.arguments,
    argumentNormalization: call.argumentNormalization, readOnly: true,
    resolvedName: call.resolvedName, capabilityId: call.capabilityId, ...(stable ? { historyId: id } : {}) }, running: false, children: [] };
}

function expandReads(items: Item[]): Item[] {
  return items.flatMap((item): Item[] => item.t === "reads"
    ? item.tools.map((tool, index) => ({ t: "tool", id: tool.historyId ?? `${item.id}/read/${index}`, tool, running: false, children: [] }))
    : [item]);
}

export function prependHistory(state: SessionState, incoming: { items: Item[]; executions: Executions }): SessionState {
  const current = expandReads(state.items);
  const older = expandReads(incoming.items);
  const known = new Map(current.map((item) => [item.id, item]));
  const inserted = new Set(older.map((item) => item.id));
  const items = [...older.map((item) => known.get(item.id) ?? item), ...current.filter((item) => !inserted.has(item.id))];
  return { ...state, items: foldHistoryItems(items), executions: { ...incoming.executions, ...state.executions } };
}
