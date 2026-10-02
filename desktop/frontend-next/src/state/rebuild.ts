import type { Item, PlanStep, SessionState, TurnTerminal } from "./session_types";
import type { Executions } from "./executions";
import { promptOpen } from "./prompts";

// A rebuild re-reads the record while a turn may still be writing it. The
// kernel commits each assistant round at its boundary, before its calls run,
// so the record can already hold a call the live side holds open; the merge
// goes by identity, the record winning where it is complete and the live card
// where it is not.
export function rebuild(s: SessionState, ev: { items: Item[]; plan?: PlanStep[]; executions: Executions }): SessionState {
  // Extensions and open prompts are not in the record; both belong to this
  // pane until it is rebound, running or not.
  const tail = s.items.filter(
    (i) =>
      i.t === "extension" ||
      promptOpen(i) ||
      (s.running && ((i.t === "say" && !i.done) || (i.t === "tool" && i.running))),
  );
  const calls = new Map<string, Extract<Item, { t: "tool" }>>();
  for (const i of tail) {
    if (i.t === "tool" && i.running && i.tool.id) calls.set(i.tool.id, i);
  }
  const openCalls = tail.some(
    (i) => i.t === "tool" && i.running && i.tool.id && !ev.items.some((r) => r.t === "tool" && r.tool.id === i.tool.id),
  );
  // A record ending on the committed answer, with no call of ours still out,
  // is a turn_done the wire lost: the answer wins and the turn is over.
  const ended = !openCalls && tail.some((i) => i.t === "say") && ev.items[ev.items.length - 1]?.t === "say";
  const items = [
    ...ev.items.map((row) => {
      if (row.t !== "tool" || !row.tool.id || !calls.has(row.tool.id)) return row;
      const open = calls.get(row.tool.id)!;
      // A bare record copy — the call without its result — yields to the live
      // card, keeping the record's id so the card stays where the record put it.
      return row.tool.output || row.tool.err ? row : { ...open, id: row.id };
    }),
    ...tail.filter((i) => {
      if (ended && i.t === "say") return false;
      if (i.t !== "tool" || !i.running || !i.tool.id) return true;
      return !ev.items.some((r) => r.t === "tool" && r.tool.id === i.tool.id);
    }),
  ];
  // How the restored turns ended is not in the record; a live turn that
  // vanished mid-flight leaves null, which is a different answer.
  const terminal: TurnTerminal = ev.items.length ? { kind: "unread" } : s.terminal;
  return { ...s, executions: ev.executions, terminal, running: s.running && !ended, items };
}
