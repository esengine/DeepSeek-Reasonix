// prompts.ts — the questions a run is stopped on, as the transcript holds them.
// An approval and an ask are the same shape here: the kernel is blocked until
// this window answers, and it re-emits the request to whoever attaches so a
// reloaded page can. What that costs the reducer is identity — the same prompt
// arrives more than once, and it must not become two answerable cards — and
// survival, because a rebuild re-reads the record and a prompt is not in it.
import type { Item, SessionState } from "./session_types";
import type { Ask, AskQuestion, DecisionReceipt } from "../port/wire";
import { IDLE, RUNNING } from "./chip";

// The id the kernel correlates an answer by, which is what makes two frames one
// prompt. Local item ids cannot: a replay mints a new one every time.
export function promptKey(it: Item): string | undefined {
  return it.t === "ask" ? it.ask.id : it.t === "approval" ? it.a.id : undefined;
}

// Sealed = answered here already. Open = the kernel is still holding the run on
// it, which is the only kind a rebuild has to carry and a replay may reopen.
export const promptSealed = (it: Item) =>
  (it.t === "ask" && it.answered !== undefined) || (it.t === "approval" && it.verdict !== undefined);

export const promptOpen = (it: Item) => promptKey(it) !== undefined && !promptSealed(it);

// The kernel replays a pending prompt to every client that attaches, and the
// frame log replays the original alongside it, so one question arrives more than
// once. Two cards are two answers it will not both accept — same id, same card,
// and the local id is kept so an answer already in flight still lands on it.
export function prompted(s: SessionState, doing: string, next: Item): SessionState {
  const key = promptKey(next);
  const at = s.items.findIndex((it) => it.t === next.t && promptKey(it) === key);
  if (at < 0) return { ...s, doing, items: [...s.items, next] };
  // A frame replayed from the log for a question already answered is history,
  // not a new wait: reopening it would ask again for a decision already made.
  if (promptSealed(s.items[at])) return s;
  const items = s.items.slice();
  items[at] = { ...next, id: s.items[at].id };
  return { ...s, doing, items };
}

// A prompt answered somewhere else — another window, a revise — is a transition,
// and the kernel records every one it settles on the same ordered stream as the
// request. That ordering is the point: the projection this used to read is a
// snapshot, and one taken before the prompt existed omits it exactly as a
// resolved one does. Sealing on that wedged the run the prompt was blocking.
export function sealByReceipt(s: SessionState, r: DecisionReceipt): SessionState {
  const at = s.items.findIndex((it) => promptKey(it) === r.id && promptOpen(it));
  if (at < 0) return s; // already sealed here, or never on this screen
  const items = s.items.slice();
  const it = items[at];
  if (it.t === "ask") items[at] = { ...it, answered: [], answeredElsewhere: true, by: r.via ?? "window", said: r.subject };
  else if (it.t === "approval") items[at] = { ...it, verdict: verdictOf(r.outcome), by: r.via ?? "window" };
  else return s;
  return { ...s, doing: it.t === "ask" ? (s.running ? RUNNING : IDLE) : s.doing, items };
}

// verdictOf picks the sealed line from the outcome the kernel recorded. The
// switch covers what it issues; an outcome added later draws the default line
// rather than being guessed into one of these.
function verdictOf(outcome: string): string {
  switch (outcome) {
    case "allow_session":
      return "always";
    case "allow_persistent":
      return "persist";
    case "deny":
    case "recovery_revise":
      return "deny";
    case "start_execution":
      return "start";
    case "revise_plan":
      return "revise";
    case "exit_plan":
      return "exit";
    case "recovery_continue_task":
      return "always";
    default:
      return "unknown";
  }
}

interface AskArgs {
  questions?: {
    header?: string;
    question?: string;
    reason?: string;
    multiSelect?: boolean;
    options?: { label?: string; description?: string }[];
  }[];
}

// A reload does not replay ask_request, so the question is read back off the
// ask call's own arguments, shaped as agent.AskTool shapes them for the wire.
export function askFromCall(id: string, args: string | undefined): Ask | null {
  let p: AskArgs;
  try {
    p = JSON.parse(args ?? "");
  } catch {
    return null;
  }
  if (!Array.isArray(p?.questions) || p.questions.length === 0) return null;
  const questions: AskQuestion[] = [];
  for (const [i, q] of p.questions.entries()) {
    if (!Array.isArray(q?.options)) return null;
    questions.push({
      id: `q${i + 1}`,
      header: q.header?.trim() ?? "",
      prompt: q.question?.trim() ?? "",
      reason: q.reason?.trim() === "missing_value" ? "missing_value" : "user_decision",
      multi: q.multiSelect === true,
      options: q.options.map((o) => ({ label: o.label?.trim() ?? "", description: o.description?.trim() || undefined })),
    });
  }
  return { id, questions };
}
