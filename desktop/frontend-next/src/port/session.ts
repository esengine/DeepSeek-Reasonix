import type { ModelMode } from "./model";
import type { Via } from "./wire";
// A conversation and the states it passes through: what is running, what it
// cost, and the points it can be wound back to.
// GET /history: the event stream is live-only, so a reload rebuilds from these.
export interface HistoryMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string;
  // Where this message sits in the session log. The rebuild drops host chrome,
  // so a row's place on screen is not its place in the record.
  msgIndex?: number;
  // The host wrote this user-role line, not the person: a goal continuation, an
  // approved plan's execution. It is the writer's own declaration — nothing
  // here decides it from the wording.
  hostAuthored?: boolean;
  // Guidance sent into a turn that was already running, rather than a turn of
  // its own. There is no checkpoint behind it, so nothing rewinds to it.
  steer?: boolean;
  // The paired device a user message was sent from; absent is the window.
  via?: Via;
  // Which model wrote this assistant turn. A rebuild has no turn_started to
  // read it off, and the composer's current setting is a different fact.
  modelRef?: string;
  reasoning?: string;
  thoughtMs?: number; // kernel-measured; absent on turns recorded before it was
  images?: number; // attachments on a user turn; an image-only one has no text
  toolCalls?: HistoryToolCall[];
  toolCallId?: string;
  toolName?: string;
  // The host's own account of a result that did not succeed. Without it a
  // rebuilt card has only the words, which a tool's own output can imitate.
  toolFailed?: boolean;
  toolRefusalCode?: string;
  // outputDiff marks a rebuilt shell result whose whole output is a unified
  // diff, re-derived by the host so a reopened transcript renders it as one.
  outputDiff?: boolean;
}

// One call inside a history message. resolvedName/capabilityId say what a
// stable proxy reached: name stays the provider-visible call, so a card built
// without them reads use_capability where the live one read the capability.
export interface HistoryToolCall {
  id: string;
  name: string;
  arguments?: string;
  resolvedName?: string;
  capabilityId?: string;
}

// GET /todos as internal/frontend/serve writes it: the canonical task list, the latest
// todo_write merged with every complete_step advance. Deriving it from the
// transcript instead loses the advances and keeps the refused writes.
// A tab the agent's browser has open. target is the browser's own id for the
// page, which the window draws its view by; id is what the agent calls it.
export interface BrowserTab {
  id: string;
  target: string;
  url: string;
  title: string;
  active: boolean;
}

export interface HostTodo {
  content: string;
  status: string;
  activeForm?: string;
  level?: number;
}

// GET /checkpoints as internal/frontend/serve writes it: the snapshot the kernel took
// before each turn's first write. prompt is the user's own text, with the
// compose prefixes already stripped kernel-side.
export interface Checkpoint {
  turn: number;
  stamp?: string;
  canFork?: boolean;
  // True only for a checkpoint copied into a fork without file snapshots.
  forkCopied?: boolean;
  prompt: string;
  files: number;
  // The session index of the user message this snapshot was taken for, and the
  // point a conversation rewind truncates at. turn numbers the snapshots, not
  // the conversation, so this is what a transcript row is joined to.
  msgIndex?: number;
}

// Only edit-tool writes are snapshotted, so "code" restores those files and
// nothing else — a shell command's side effects are not recoverable.
export type RewindScope = "code" | "conversation" | "both";

// What POST /rewind/prepare answers: the plan the kernel would apply, plus what
// it cannot reach. requiresConfirmation is true when coverage is partial — the
// turn also changed things outside the snapshot, typically via bash.
// What POST /rewind/commit answers. transactionId is what undo needs, and
// undoAvailable says whether the kernel can still reverse it.
export interface RewindFileStage {
  path: string;
  phase: string;
  action?: string;
  error?: string;
  compensated?: boolean;
  compensateError?: string;
}

export interface RewindResult {
  ok?: boolean;
  transactionId?: string;
  undoAvailable?: boolean;
  deleted?: string[];
  written?: string[];
  files?: RewindFileStage[];
  conversationOk?: boolean;
  error?: string;
  conflicts?: RewindConflict[];
  coverage?: string;
  coverageGaps?: { reason: string; detail: string; tool?: string }[];
}

export interface RewindPlan {
  planId: string;
  turn: number;
  coverage: string;
  coverageGaps?: { reason: string; detail: string; tool?: string }[];
  canFiles: boolean;
  canConversation: boolean;
  disabledReason?: string;
  files?: string[];
  fileCount: number;
  requiresConfirmation: boolean;
  // Single-file revert only: which file, and whether it has moved since the
  // checkpoint captured it. A conflict is the one case the caller must answer.
  path?: string;
  conflicts?: RewindConflict[];
}

// Why one file cannot be put back without a decision.
export interface RewindConflict {
  path: string;
  reason: string;
  currentExisted?: boolean;
}

export type ApprovalMode = "ask" | "auto" | "dontAsk" | "yolo";

export type WorkspaceTrust = "" | "trusted" | "declined";

/** Where the session's posture came from. A defaulted session opens in auto
 *  only when the OS sandbox confines writes and the folder is trusted. */
export interface ApprovalDefault {
  defaulted: boolean;
  writesConfined: boolean;
  trust: WorkspaceTrust;
  trustable: boolean;
}

// "light" was retired into balanced: its only enforced differences were two
// sub-agent switches, and a setting that costs a choice without changing what
// it names is a question not worth asking. Old sessions still send it.
export type Preset = "light" | "balanced" | "delivery";

// What an answer authorises. "session" covers the calls after it until this
// session ends; "always" also writes it down as a rule. Which of them the host
// will honour rides on the request — see Approval.allowsSession/allowsPersist.
export type ApprovalVerdict = "once" | "session" | "always" | "deny";

// Shape of GET /status as internal/frontend/serve writes it. Anything the UI wants that
// is not here has to be added on the Go side, not invented in the client.
// PlanAction is what a plan card can answer. The three are distinct kernel
// transitions, not an allow/deny pair: revising keeps planning, exiting leaves
// the workflow, and only starting carries the plan into execution.
export type PlanAction = "start" | "revise" | "exit";

// PLAN_ACTIONS maps them to the names the kernel answers to.
export const PLAN_ACTIONS: Record<PlanAction, string> = {
  start: "start_execution",
  revise: "revise_plan",
  exit: "exit_plan",
};

// PlanPhase is where the run sits in the plan lifecycle. Absent from a status
// means outside it; there is deliberately no "inactive" string to render.
export type PlanPhase = "planning" | "awaiting_approval" | "executing";

// AskReason is why only the user can answer. The two values are the whole set:
// permission and plan approval are different decisions with different owners.
export type AskReason = "user_decision" | "missing_value";

export interface DecisionOption {
  label: string;
  description?: string;
}

export interface DecisionQuestion {
  id: string;
  header?: string;
  prompt: string;
  reason?: AskReason;
  options: DecisionOption[];
  multi?: boolean;
}

// A Decision is something waiting on the user, named by the id its owner
// issued. The id goes back untouched — the frontend never describes the state
// it thinks the run is in, and never resolves a decision itself: it disappears
// when the next projection no longer carries it.
//
// Every open prompt is in the list, and kind says which call answers it. That
// completeness is what makes a card missing from it mean "answered elsewhere"
// rather than "this kind is not projected".
export interface Decision {
  id: string;
  kind: "plan_approval" | "tool_approval" | "recovery_approval" | "ask";
  questions?: DecisionQuestion[];
}

export interface SessionStatus {
  // Whether the current model reads images at all, and whether that answer was
  // ever given: a relay forwards models nothing here has a label for, and an
  // undeclared one is not the same claim as a text-only one.
  vision?: boolean;
  visionDeclared?: boolean;
  label: string;
  running: boolean;
  // plan drives the composer toggle and nothing else: it is the legacy
  // projection, false once a plan is approved. planPhase is the lifecycle, and
  // it is absent when the run is outside the workflow entirely.
  plan: boolean;
  planPhase?: PlanPhase;
  decisions?: Decision[];
  preset: Preset;
  effort?: string;
  // Absent unless the session's model declares a mode: its switch is not drawn.
  modes?: ModelMode[];
  modelRef?: string;
  // The label the user gave modelRef's provider; absent when none is set.
  providerDisplayName?: string;
  toolApprovalMode: ApprovalMode;
  // Absent from a kernel that predates it; drawn only where it is present.
  approvalDefault?: ApprovalDefault;
  autoApproveTools: boolean;
  bypass: boolean;
  goal: string;
  goalStatus: string;
  cwd: string;
  workspaceRoot?: string;
  sessionPath?: string;
  used: number;
  window: number;
  cacheHit: number;
  cacheMiss: number;
  sessionCostQuote?: import("./wire").CostQuote;
  jobs?: JobEntry[];
}

/** One currency of a wallet, rendered where the symbol rules live. Two
 *  currencies are two lines and never a sum — combining them would mean
 *  inventing an exchange rate — so the only thing done here is stack them. */
export interface WalletLine {
  currency: string;
  total: string;
  granted?: string;
}

/** What the provider's wallet says, and when it said it. A value standing in
 *  past its freshness says so rather than looking current — the endpoint being
 *  briefly unreachable is not the account being empty. */
export interface WalletReading {
  display: string;
  available: boolean;
  stale: boolean;
  fetchedAt: string;
  lines?: WalletLine[];
}

export interface JobEntry {
  id: string;
  kind: string;
  label: string;
  status: string;
  startedAt: number;
}

// The UI depends on this and nothing else. SsePort talks to internal/frontend/serve;
// MockPort replays a fixture. Neither is allowed to leak transport details
// upward, which is what keeps the same UI usable in a browser and in a shell.
export interface SessionEntry {
  name: string;
  path: string;
  title?: string;
  turns?: number;
  current?: boolean;
}
