// What a session is made of, apart from the reducer that maintains it: the
// rows the transcript draws and the state one turn hands the next.
import type { Ask, Approval, Compaction, ExtensionSurface, Guardian, Receipt, Tool } from "../port/wire";
import type { Sample } from "../port/tokens";

export type Item =
  // itemId names the durable queue entry while pending, which is what a
  // cancel asks the kernel to drop. queued says which wait it is in: guidance
  // lands at the next tool boundary, a follow-up when this turn is done.
  | { t: "user"; id: string; text: string; pending?: boolean; itemId?: string; queued?: "steer" | "followup" }
  | { t: "say"; id: string; text: string; reasoning?: string; done: boolean; thoughtMs?: number }
  | { t: "tool"; id: string; tool: Tool; running: boolean; children: Tool[] }
  | { t: "reads"; id: string; tools: Tool[] }
  | { t: "guardian"; id: string; g: Guardian }
  | { t: "approval"; id: string; a: Approval; verdict?: string }
  | { t: "ask"; id: string; ask: Ask; answered?: string[][] }
  | { t: "compaction"; id: string; c: Compaction; done: boolean }
  | { t: "remember"; id: string; m: RememberedFact; forgotten?: boolean }
  | { t: "receipt"; id: string; r: Receipt }
  | { t: "extension"; id: string; ext: ExtensionSurface }
  // code identifies what the kernel is reporting. text is its own English,
  // kept as the fallback for a code this build has no wording for.
  | { t: "notice"; id: string; level: string; text: string; detail?: string; code?: string; count?: number };

// What a remember call wrote, read off its own arguments. Saving a fact changes
// what the agent will do in later sessions, which no other tool call does — so it
// gets a card of its own rather than scrolling past as one more step.
export interface RememberedFact {
  name: string;
  title: string;
  description: string;
  scope: string;
  activation: string;
  body: string;
}

export interface Metrics {
  hit: number;
  miss: number;
  out: number;
  bySource: Record<string, number>;
  cost: number;
  currency: string;
  // The prefix the last round was sampled against, and whether it moved. A
  // cache that stops hitting is almost always a prefix that changed, and the
  // hash is what tells "changed" from "the endpoint dropped it".
  prefixHash: string;
  prefixChanged: boolean;
  prefixReasons: string[];
  // The other half of that answer. The prefix hash alone cannot tell a dropped
  // cache from a rewritten conversation, because the conversation is not in it:
  // bodyChanged says whether the carried messages are the bytes last sent.
  bodyChanged: boolean;
  carriedMessages: number;
  // Tool schemas are the third thing filling a prefix and the one nobody
  // counts, so it is reported rather than inferred from the context breakdown.
  toolSchema: number;
  // The quote's own confidence. A number billed at a published price and one
  // estimated from a fallback table are different claims.
  estimated: boolean;
  // The converted amount, when the host quoted in a second currency. Kept
  // apart rather than summed: adding them would require inventing a rate.
  alt: { amount: number; currency: string } | null;
  // What this turn added, and the per-round series behind the trend. Bounded:
  // a long session must not grow an unbounded array in the reducer.
  turn: number;
  rounds: number[];
}

export interface Waiting {
  ttftSince?: number;
  // scope is the kernel's own answer to which half of the request broke, and
  // since is when the stall began rather than when this attempt did — the
  // attempt counter already says how far into it the run is.
  retry?: { attempt: number; max: number; scope?: "headers" | "stream"; since: number };
}

export interface PlanStep {
  text: string;
  done: boolean;
}

export interface RuntimeNotice {
  id: string;
  level: string;
  // The stable id a window says this in its own language by; text is what the
  // kernel wrote for a terminal, and the fallback when nothing maps the code.
  code?: string;
  text: string;
  detail?: string;
}

/** TurnTerminal is how the turn in front of you ended, or null while it has not.
 *  Four judgements rather than a flag: only one of them is a delivery, and a
 *  turn that stopped short of its obligations reads nothing like one that
 *  finished. The label beside it is presentation and says less than this. */
export type TurnTerminal =
  | { kind: "completed" }
  | { kind: "failed"; err: string }
  | { kind: "cancelled" }
  | { kind: "incomplete"; outcome: string }
  | null;

export interface SessionState {
  error: string;
  // How this turn ended, cleared when the next one starts.
  terminal: TurnTerminal;
  // Notices about the machine running this conversation rather than about the
  // conversation. They describe something that is still true — which model was
  // resolved, which server failed to start — so, like a standing extension
  // surface, they hold their own place instead of scrolling away in the
  // transcript as if they had been said by someone.
  runtime: RuntimeNotice[];
  items: Item[];
  // Bumped when the transcript's composition changes — a card added, settled,
  // folded, answered — but NOT when a message still being written grows by a
  // chunk. Everything derived from the tool and user cards (the rail's panels,
  // the step count, the checkpoint pairing) can key off this and skip the
  // hundreds of frames a single answer streams in. A derivation that reads the
  // *text* of a card must key off items instead.
  revision: number;
  plan: PlanStep[];
  // Recent streamed-output samples. The down-rate has to come from what arrived
  // in the last few seconds; usage totals only land at round boundaries, and
  // nothing at all arrives while a tool runs.
  outWindow: Sample[];
  metrics: Metrics;
  waiting: Waiting;
  running: boolean;
  doing: string;
  steerQueue: string[];
  // How many times the kernel has said the durable queue moved. The frame
  // carries nothing else on purpose — one authority answers what is in there,
  // and this is what asks it again. Counting, not the kernel's own revision:
  // the point is that it changed, and a counter cannot arrive out of order.
  queueMoved: number;
  // Standing extension surfaces, keyed by plugin and surface id. They describe
  // a state that is still true, so they hold a place in the side rail instead
  // of scrolling away in the transcript.
  panels: ExtensionSurface[];
  // Composed views. A view is a standing surface by definition — it describes
  // something that is still true — so it never joins the transcript, and where
  // it is drawn is decided at render time rather than here. That is what lets
  // the user move one without any of this having to be re-sorted.
  views: ExtensionSurface[];
  // Views that replace a card the host would have drawn, keyed by anchor. They
  // are kept apart from `views` because they have no place of their own: they
  // appear only where the thing they stand in for appears.
  takeovers: Record<string, ExtensionSurface>;
}
