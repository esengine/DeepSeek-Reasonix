// Mirrors internal/eventwire. Field names and kind strings must match exactly;
// this file is the contract, not a convenience shape.

export type Kind =
  | "turn_started"
  | "reasoning"
  | "text"
  | "message"
  | "tool_dispatch"
  | "tool_result"
  | "tool_progress"
  | "usage"
  | "notice"
  | "phase"
  | "turn_phase"
  | "approval_request"
  | "ask_request"
  | "turn_done"
  | "compaction_started"
  | "compaction_progress"
  | "compaction_done"
  | "mcp_surface_ready"
  | "retrying"
  | "steer"
  | "guardian_assessment"
  | "extension_surface"
  | "extension_status"
  | "stream_attempt"
  | "context_maintenance"
  | "todo_progress"
  | "workspace_changed"
  | "completion_summary"
  | "inbox_changed"
  | "adjudications_changed"
  | "graph_delta"
  // Transport frames, not kernel events: the stream describing itself. Handled
  // in the port and never reaching the reducer.
  | "stream_gap"
  | "stream_watermark";

// Present when a call hands work to sub-agents: who runs it, how many of them,
// and the model/effort they resolved to. Its presence is what marks a
// delegation — the model only ever calls use_capability, so the tool name does
// not say whether the work left this context.
export interface Profile {
  name?: string;
  count?: number;
  model?: string;
  effort?: string;
}

// Present on shell tools. A non-zero exit was invisible before: the card only
// ever rendered stdout, so a command that failed looked like one that ran.
export interface Execution {
  kind?: string;
  shell?: string;
  platform?: string;
  state?: string;
  failurePhase?: string;
  exitCode?: number;
  outputTail?: string;
  mutationRisk?: string;
  verification?: string;
  durationMs?: number;
  contextTokens?: number;
}

// How a result was fitted into the model's context, absent when it arrived
// whole. The distinction is what a card has to show: spilled and windowed are
// still reachable, truncated is content the model never saw.
export interface Bound {
  kind: "spilled" | "windowed" | "truncated";
  lines?: number;
  bytes?: number;
  keptBytes?: number;
  path?: string;
}

export interface Tool {
  id?: string;
  name: string;
  args?: string;
  resolvedName?: string;
  capabilityId?: string;
  output?: string;
  err?: string;
  readOnly: boolean;
  // truncated is the pre-Bound projection kept for old journals; read bound.
  truncated?: boolean;
  bound?: Bound;
  durationMs?: number;
  contextTokens?: number;
  startedAt?: number;
  endedAt?: number;
  partial?: boolean;
  // Cumulative argument characters received so far on a partial dispatch — the
  // only liveness a streaming payload has before its JSON parses.
  argChars?: number;
  refreshed?: boolean;
  parentId?: string;
  attemptId?: string;
  diff?: string;
  added?: number;
  removed?: number;
  profile?: Profile;
  execution?: Execution;
}

// Only prefixChangeReasons is omitempty on the wire; the rest always arrive, so
// marking them optional would make every reader handle an absence that the
// producer never sends.
export interface CacheDiagnostics {
  prefixHash: string;
  prefixChanged: boolean;
  prefixChangeReasons?: string[];
  toolSchemaTokens: number;
  cacheMissTokens: number;
  cacheHitTokens: number;
  // Whether the messages this request carried over are the bytes the last one
  // sent. A miss with neither the prefix nor the body changed is the
  // provider's, and the panel has to be able to say so.
  carriedMessages: number;
  bodyChanged: boolean;
  bodyHash: string;
}

export interface Money {
  amount: string;
  currency: string;
}

export interface CostQuote {
  original: Money;
  selected?: Money;
  estimated: boolean;
  costComplete: boolean;
}

export interface Usage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  reasoningTokens?: number;
  estimated?: boolean;
  source?: string;
  // The stream attempt that billed these tokens. Usage lands after its round
  // has settled, so this is what attaches it — not whatever is still open.
  attemptId?: string;
  cacheDiagnostics?: CacheDiagnostics;
  sessionCacheHitTokens: number;
  sessionCacheMissTokens: number;
  // Context* is the latest single request's shape, for gauges and rebind. Absent
  // means fall back to the billable prompt/completion totals above.
  contextPromptTokens?: number;
  contextCompletionTokens?: number;
  contextReasoningTokens?: number;
  contextCacheHitTokens?: number;
  contextCacheMissTokens?: number;
  cost?: number;
  currency?: string;
  currencyCode?: string;
  costQuote?: CostQuote;
}

export interface Approval {
  id: string;
  tool: string;
  subject: string;
  reason?: string;
  fresh?: boolean;
  kind?: "tool" | "plan" | "recovery";
}

// DecisionReceipt is what the kernel recorded when a prompt was settled, on the
// same ordered stream as the request that raised it. Outcome is its vocabulary,
// not the buttons': a window that did not click is told what the click was.
export interface DecisionReceipt {
  id: string;
  kind: string;
  tool?: string;
  subject?: string;
  outcome: string;
}

export interface AskOption {
  label: string;
  description?: string;
}

// No json tags on event.AskAnswer, so the wire keys are the Go field names.
export interface AskAnswer {
  QuestionID: string;
  Selected: string[];
}

export interface AskQuestion {
  id: string;
  header?: string;
  prompt: string;
  // Why only the user can answer. Two values, and a renderer that switches on
  // them exhaustively: a third would be a decision with a different owner.
  reason?: "user_decision" | "missing_value";
  options: AskOption[];
  multi?: boolean;
}

export interface Ask {
  id: string;
  questions: AskQuestion[];
}

export interface Guardian {
  id: string;
  tool: string;
  subject: string;
  outcome: string;
  risk_level?: string;
  user_authorization?: string;
  rationale?: string;
  duration_ms?: number;
}

// Extension UI surfaces. A sidecar publishes data, never markup — the host
// decides how to draw it, which is why the same extension shows up as a card
// here, as text in the CLI, and as a client-rendered block over ACP.
export interface ExtensionStatus {
  label: string;
  detail?: string;
  severity?: string;
  progress?: number;
}

export interface ExtensionKeyValue {
  key: string;
  value: string;
}

export interface ExtensionActionRef {
  actionId: string;
  label: string;
}

export interface ExtensionCard {
  title?: string;
  markdown?: string;
  text?: string;
  fields?: ExtensionKeyValue[];
  progress?: number;
  actions?: ExtensionActionRef[];
}

export interface ExtensionFormField {
  key: string;
  label?: string;
  kind?: "confirm" | "input" | "select" | "multiselect";
  options?: string[];
  default?: unknown;
  required?: boolean;
}

export interface ExtensionForm {
  title?: string;
  message?: string;
  fields: ExtensionFormField[];
}

// A panel has no markdown on purpose — see the protocol DTO: the side rail is
// a narrow column, and a rendered document there costs more than it tells.
export interface ExtensionPanel {
  title?: string;
  text?: string;
  fields?: ExtensionKeyValue[];
  progress?: number;
  actions?: ExtensionActionRef[];
}

export interface ExtensionNotification {
  title: string;
  body?: string;
  severity?: string;
}

// A view is composed rather than filled in: the extension sends a tree of
// primitives and this side renders them with its own components. Tone says
// what a node means, never what colour it is — the palette stays ours.
export type ExtensionViewTone = "dim" | "strong" | "ok" | "warn" | "err" | "accent";

export interface ExtensionViewNode {
  kind: "text" | "markdown" | "row" | "stack" | "kv" | "meter" | "pip" | "button" | "divider";
  value?: string;
  key?: string;
  label?: string;
  tone?: ExtensionViewTone;
  progress?: number;
  actionId?: string;
  children?: ExtensionViewNode[];
}

export interface ExtensionView {
  // Where the extension would like to stand. A name we do not know renders
  // where we put views we have no place for, rather than not at all.
  slot?: string;
  // "tool:<callId>" when this view replaces a card's body instead of standing
  // on its own. Only tool calls can be anchored — an approval prompt or an
  // error state is not addressable, which is what keeps a takeover from being
  // able to redraw a decision.
  anchor?: string;
  body: ExtensionViewNode[];
}

export interface ExtensionSurface {
  pluginId: string;
  surfaceId: string;
  sessionId?: string;
  generation?: number;
  kind: "status" | "card" | "form" | "notification" | "panel" | "view";
  status?: ExtensionStatus;
  card?: ExtensionCard;
  form?: ExtensionForm;
  notification?: ExtensionNotification;
  panel?: ExtensionPanel;
  view?: ExtensionView;
}

// One task-list transition. kind is the structural verdict; the three revisions
// count writes, plan changes, and execution movement separately, because a turn
// can produce any number of the first two without the third ever moving.
export interface TodoProgress {
  kind: string;
  steps?: number;
  completed?: number;
  contentRevision?: number;
  planRevision?: number;
  progressRevision?: number;
}

// One context-maintenance transaction. status "noop" carries code: the attempt
// ran and freed nothing, and code is what tells "already folded this turn" from
// "nothing left to fold" — they read alike and mean opposite things about what
// the next round can expect. boundary names which threshold was reached.
export interface ContextMaintenance {
  status?: string;
  action?: string;
  trigger?: string;
  operationId?: string;
  code?: string;
  boundary?: string;
  triggerTokens?: number;
  inputTokens?: number;
  resultTokens?: number;
  savedTokens?: number;
  affectedToolResults?: number;
  projectionVersion?: number;
  cacheBreak?: boolean;
  reason?: string;
}

export interface Compaction {
  trigger?: string;
  messages?: number;
  summary?: string;
  // What the fold cost, and what the digest kept of it. coverageRequired counts
  // the changes and failures the folded work produced; coverageMissing is how
  // many the digest did not carry.
  sourceTokens?: number;
  projectionTokens?: number;
  coverageRequired?: number;
  coverageMissing?: number;
  coverageRepaired?: boolean;
  // The host wrote the dropped facts in itself, because the digest did not.
  coverageBackstopped?: boolean;
}

export interface StreamAttempt {
  id: string;
  action: "begin" | "discard" | "commit";
  attempt?: number;
  max?: number;
  reason?: string;
}

export interface CompletionSummary {
  preset: string;
  verdict: string;
  mutations: number;
  checks_passed: number;
  checks_failed: number;
  checks_suppressed: number;
  review: string;
  gap_kinds?: string[];
  // Existing tests the turn rewrote or removed, which is why the pass count
  // above may not mean what it did last turn.
  criteria_rewritten?: string[];
}

// MemoryCitation is one local memory the turn drew on, so an answer can show
// what it was grounded in rather than asserting it.
export interface MemoryCitation {
  id?: string;
  source: string;
  lineStart?: number;
  lineEnd?: number;
  note?: string;
  kind?: string;
}

// The user-facing completion record on turn_done: what the host could verify
// about the turn's work and, the part no prose reliably carries, what it could
// not. The quality summary beside it is a machine record and belongs in the
// trajectory; this is the one written for a person.
export interface Receipt {
  verdict: string;
  changes?: ReceiptChange[];
  verifications?: ReceiptVerification[];
  gaps?: ReceiptGap[];
  // Declarations the turn volunteered, kept apart from the gaps the host found.
  // Folding them together reads a caveat somebody offered as a failure.
  risks?: string[];
  unverified?: string[];
  // The kernel's own answer to "is this worth showing at all", carried rather
  // than recomputed here so the windows and the terminal never drift apart.
  saysSomething?: boolean;
}

export interface ReceiptChange {
  path: string;
  reviewed: boolean;
}

// stale: it ran before the newest change, so it proves nothing about the tree
// as it stands. inconclusive: the shell reported a later stage's status.
export interface ReceiptVerification {
  command: string;
  passed: boolean;
  stale?: boolean;
  inconclusive?: boolean;
}

export interface ReceiptGap {
  kind: string;
  detail?: string;
}

// The run's execution graph, published as facts rather than drawn from id
// prefixes. Mirrors internal/agentgraph: a node named again updates the one
// already there, and an edge kind is what tells "waited for this" apart from
// "started from this answer".
export type GraphNodeKind = "group" | "worker" | "external";

export type GraphNodeState =
  | "pending"
  // Ready and waiting for a session concurrency slot. Apart from "pending" on
  // purpose: nothing upstream is missing, only the ceiling stands in the way.
  | "queued"
  | "running"
  | "completed"
  | "adopted"
  | "failed"
  | "cancelled"
  | "skipped";

export type GraphEdgeKind = "spawn" | "depends" | "context" | "adopt";

// Absent means the question does not apply: a group is not a run, and an
// adopted node never got a grant. A bool could not say that apart from "writes".
export type GraphGrant = "read" | "write";

// Why a queued node is not running. "queued" is one word for constraints with
// different answers: the first two are session ceilings, and a claim is a write
// path someone else holds, which no ceiling releases.
export type GraphWait = "slots" | "writers" | "claim";

export interface GraphNode {
  id: string;
  parentId?: string;
  kind?: GraphNodeKind;
  state?: GraphNodeState;
  label?: string;
  profile?: string;
  model?: string;
  effort?: string;
  grant?: GraphGrant;
  wait?: GraphWait;
  ref?: string;
  queuedAt?: number;
  startedAt?: number;
  endedAt?: number;
  err?: string;
}

export interface GraphEdge {
  from: string;
  to: string;
  kind: GraphEdgeKind;
}

export interface GraphDelta {
  nodes?: GraphNode[];
  edges?: GraphEdge[];
}

// The folded graph, as the kernel's durable facts justify it. Same two arrays a
// delta carries and a different thing entirely: this one is a state, not a
// publication, and it is replaced rather than folded.
export interface ExecutionGraph {
  nodes?: GraphNode[];
  edges?: GraphEdge[];
}

// One execution the host found open with nobody running it. kind separates work
// that may be half-done from work that never began; neither resumes.
export interface ExecutionInterruption {
  execution: string;
  kind: "interrupted-before-start" | "interrupted-during-execution";
}

// The whole execution read model, and the only authority for the two lists
// beside the graph: no delta carries them, so nothing can patch them in later.
export interface ExecutionGraphSnapshot {
  graph: ExecutionGraph;
  interruptions?: ExecutionInterruption[];
  // Executions whose worker layer was never recorded. Their model reads empty
  // because nothing was observed, which an inherited one does not.
  identityUnknown?: string[];
}

// The envelope the snapshot arrives in: which conversation it describes, and
// the frame it is at least as new as.
export interface ExecutionGraphView {
  schemaVersion: number;
  sessionId: string;
  watermark: number;
}

export type ExecutionGraphRead = ExecutionGraphView & ExecutionGraphSnapshot;

export interface WireEvent {
  kind: Kind;
  text?: string;
  detail?: string;
  code?: string;
  reasoning?: string;
  level?: string;
  // "operator" means the notice is about the machine running this conversation,
  // not about the conversation. Only the latter belongs in a transcript.
  audience?: string;
  tool?: Tool;
  usage?: Usage;
  approval?: Approval;
  ask?: Ask;
  guardian?: Guardian;
  extension?: ExtensionSurface;
  compaction?: Compaction;
  maintenance?: ContextMaintenance;
  todoProgress?: TodoProgress;
  streamAttempt?: StreamAttempt;
  completion?: CompletionSummary;
  graph?: GraphDelta;
  receipt?: Receipt;
  decisionReceipt?: DecisionReceipt;
  err?: string;
  // turn_done: the user asked to stop. A cancelled turn and a dropped
  // connection both end with a context-canceled err; only this tells them apart.
  cancelled?: boolean;
  memoryCitations?: MemoryCitation[];
  // The turn a compaction checkpoint committed under, so a reader can tell a
  // fold apart from the turns around it.
  checkpointTurn?: number;
  // recovery_paused is no longer emitted; sessions recorded before the retry
  // budgets were removed still carry it, so a reader has to render it.
  outcome?: "final_readiness" | "recovery_paused";
  phase?: string;
  retryAttempt?: number;
  retryMax?: number;
  retryScope?: "headers" | "stream";
  itemId?: string;
  // Set by the transport on frames that must not be missed, so a client can
  // tell it missed one. Streaming deltas carry none — losing one costs nothing
  // the next frame does not restate.
  seq?: number;
}
