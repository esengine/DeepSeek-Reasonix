// What a session is made of, apart from the reducer that maintains it: the
// rows the transcript draws and the state one turn hands the next.
import type { Ask, Approval, Compaction, CostCoverage, ExtensionSurface, Guardian, Receipt, Tool, Via, WorkspaceLease } from "../port/wire";
import type { Sample } from "../port/tokens";
import type { Executions } from "./executions";

export type Item =
  // itemId names the durable queue entry while pending, which is what a
  // cancel asks the kernel to drop. queued says which wait it is in: guidance
  // lands at the next tool boundary, a follow-up when this turn is done.
  // authoredTurn/msgIndex are the kernel's names for this message, stamped by
  // the turn_started it caused (live) or read off the record (a rebuild). A row
  // that has neither was never a turn of its own.
  | {
      t: "user";
      id: string;
      text: string;
      pending?: boolean;
      // Said into a turn already running. It never was a turn of its own, so it
      // has no checkpoint and nothing rewinds to it.
      steer?: boolean;
      itemId?: string;
      queued?: "steer" | "followup";
      authoredTurn?: number;
      msgIndex?: number;
      // The paired device it was sent from; absent is the window.
      via?: Via;
    }
  // source names which model wrote it — a turn can carry two, and the card is
  // where the answer has to survive.
  | { t: "say"; id: string; text: string; reasoning?: string; done: boolean; thoughtMs?: number; source?: string; model?: string }
  | { t: "tool"; id: string; tool: Tool; running: boolean; children: Tool[] }
  | { t: "reads"; id: string; tools: Tool[] }
  | { t: "guardian"; id: string; g: Guardian }
  // by is who settled it when that was not this screen: the device named on
  // the kernel's receipt, or the window when the receipt names none.
  | { t: "approval"; id: string; a: Approval; verdict?: string; by?: Via | "window" }
  // An ask answered on another screen keeps who answered it and what the
  // kernel recorded they said, since this screen never saw the selection.
  // recorded is the call's result when the card is rebuilt from history: the
  // record keeps what the model was told, not which options were pressed.
  | { t: "ask"; id: string; ask: Ask; answered?: string[][]; answeredElsewhere?: boolean; by?: Via | "window"; said?: string; recorded?: string }
  | { t: "compaction"; id: string; c: Compaction; done: boolean }
  | { t: "remember"; id: string; m: RememberedFact; forgotten?: boolean }
  | { t: "receipt"; id: string; r: Receipt }
  | { t: "extension"; id: string; ext: ExtensionSurface }
  // code identifies what the kernel is reporting. text is its own English,
  // kept as the fallback for a code this build has no wording for.
  | { t: "notice"; id: string; level: string; text: string; detail?: string; code?: string; count?: number; workspaceLease?: WorkspaceLease };

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
  // How much of what this session billed carries a price fact. A session that
  // has spent nothing yet and one whose rounds went unpriced both total zero,
  // and the amount cannot tell them apart — only this can.
  coverage: CostCoverage;
  // Why a round fell short, carried for provenance. coverage is the state; this
  // never stands in for it.
  incompleteReason: string;
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

/** complete_step moves an item to completed and promotes the next one itself,
 *  so which step is underway is the kernel's answer. A list it has not started
 *  has no current step, which is not the first one being in hand. */
export type TodoStatus = "pending" | "in_progress" | "completed";

export interface PlanStep {
  text: string;
  status: TodoStatus;
  /** The "-ing" wording the kernel keeps for while this is the step in hand. */
  activeForm?: string;
  /** Depth in the kernel's list. Sub-steps read as peers without it. */
  level?: number;
}

export const stepDone = (s: PlanStep) => s.status === "completed";

/** Which step the kernel says is underway, or -1. Never "the first one not
 *  ticked": the host can leave earlier items pending and start a later one. */
export const currentStep = (steps: PlanStep[]) => steps.findIndex((s) => s.status === "in_progress");

export const stepLabel = (s: PlanStep) => (s.status === "in_progress" && s.activeForm?.trim() ? s.activeForm : s.text);

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
  // A transcript read back from the record: the frame that said how the last
  // turn ended is not in it, and neither a delivery nor a stop can be claimed.
  | { kind: "unread" }
  | null;

export interface Stall {
  cause: "rounds" | "tokens" | "perseveration";
  idleRounds: number;
  roundLimit: number;
  promptTokens: number;
  tokenMultiple: number;
  // The user's pause setting ended the run on this reading.
  paused: boolean;
}

export interface SessionState {
  error: string;
  // What the tool calls of this session did, keyed by wire id. Held apart from
  // items because the transcript folds, nests and replaces the cards drawing
  // them, and none of that is a change to what ran.
  executions: Executions;
  // How this turn ended, cleared when the next one starts.
  terminal: TurnTerminal;
  // Notices about the machine running this conversation rather than about the
  // conversation. They describe something that is still true — which model was
  // resolved, which server failed to start — so, like a standing extension
  // surface, they hold their own place instead of scrolling away in the
  // transcript as if they had been said by someone.
  runtime: RuntimeNotice[];
  // The kernel's reading that the run stopped producing observable effects,
  // and whether this window was asked to stop saying so while it runs.
  stall?: Stall;
  stallMuted?: boolean;
  items: Item[];
  // Which cards have not yet had their one entrance, and how far this
  // projection has already let in. Motion describes a state transition, and
  // the transition here is a fact arriving — not a DOM node being created. A
  // card mounts again every time virtualization brings its block back, and
  // replaying that motion says the hour-old thing just happened.
  //
  // Presentation bookkeeping, deliberately beside Item rather than on it: what
  // a card is does not depend on whether its arrival has been shown yet.
  // The model the running turn was started on. A reply names its own when a
  // second model wrote it; the rest are this one, recorded when the turn began
  // rather than read off the composer, which moves on.
  turnModel?: string;
  entranceOwed: string[];
  enteredThrough: number;
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
  // What this round has produced so far, estimated from the chunks themselves,
  // because usage only lands when the round closes. It is cleared by that usage
  // event, which replaces the estimate with what was billed — a reading that is
  // still an estimate says so rather than passing for the measured one.
  outLive: number;
  metrics: Metrics;
  waiting: Waiting;
  running: boolean;
  doing: string;
  steerQueue: string[];
  // The rows whose turns have not started yet, oldest first: each send that the
  // kernel has not yet named a message for. A steer never joins them, because
  // it starts no turn of its own.
  awaitingTurnStart: string[];
  // How many times the kernel has said the durable queue moved. The frame
  // carries nothing else on purpose — one authority answers what is in there,
  // and this is what asks it again. Counting, not the kernel's own revision:
  // the point is that it changed, and a counter cannot arrive out of order.
  queueMoved: number;
  // How many times the agent's browser tabs moved; the tabs are read back.
  browserTabsMoved: number;
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
  // The last phase each sub-agent reported, by call id. Kept beside the cards
  // because a background child outlives the call that started it.
  subagentPhase: Record<string, string>;
}
