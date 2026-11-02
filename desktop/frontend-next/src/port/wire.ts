import type { ContextComposition } from "./context_composition";
import type { WorkspaceLease } from "./workspace_lease";
export type { WorkspaceLease } from "./workspace_lease";
import type { SurfaceImage, SurfaceLocation, SurfaceFact, SurfaceDataset } from "./surface-rich";
export type { SurfaceImage, SurfaceLocation, SurfaceFact, SurfaceDataset, SurfaceSeries } from "./surface-rich";
import type { GraphDelta } from "./execution_graph";
import type { MessageOrigin } from "./team";
export type {
  ExecutionGraph, ExecutionGraphRead, ExecutionGraphSnapshot, ExecutionGraphView, ExecutionInterruption,
  GraphDelta, GraphEdge, GraphEdgeKind, GraphGrant, GraphNode, GraphNodeKind, GraphNodeState, GraphWait,
} from "./execution_graph";
// Mirrors internal/contract/eventwire. Field names and kind strings must match exactly;
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
  | "ask_engaged"
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
  | "workspace_lease"
  | "workspace_changed"
  | "completion_summary"
  | "inbox_changed"
  | "rewind_changed"
  | "adjudications_changed"
  | "browser_tabs_changed"
  | "graph_delta"
  | "progress_watch"
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
  effort?: string; label?: string; // label: the task's own short description, one sub-agent only
}

// Present on shell tools. A non-zero exit was invisible before: the card only
// ever rendered stdout, so a command that failed looked like one that ran.
export interface Execution {
  kind?: string;
  shell?: string;
  /** PowerShell 5.1 and 7 are different languages; the shell name alone cannot say which answered. */
  shellVersion?: string;
  platform?: string;
  /** False on Windows PowerShell 5.1, where `&&` is a syntax error. */
  supportsAndAnd?: boolean;
  state?: string;
  failurePhase?: string;
  exitCode?: number;
  outputTail?: string;
  /** The command without its leading environment assignments, when that changed it; never what authorised it. */
  subject?: string;
  mutationRisk?: string;
  verification?: string;
  durationMs?: number;
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

/** Who initiated a tool invocation.
 *
 *  - model: the call came out of a model response's tool_calls.
 *  - host: the host invoked it itself — seeding an approved plan, advancing a
 *    step, closing a goal, expanding one delegation call into its items.
 *  - user: a person invoked it directly, by slash command or inline shell.
 *  - provider: the provider ran it on its side and reported only a result.
 *
 *  Absent on frames written before the field existed. Absent is unknown, not
 *  "model" — a fold that assumes otherwise counts the host's own bookkeeping
 *  as the model's work.
 */
export type ToolIssuer = "model" | "host" | "user" | "provider";

export interface ArgumentNormalization {
  reason: string;
  original: string;
  effective: string;
}

export interface Tool {
  deliveries?: import("./workspace").DeliveredFile[];
  id?: string;
  name: string;
  args?: string;
  argumentNormalization?: ArgumentNormalization;
  resolvedName?: string;
  capabilityId?: string;
  output?: string;
  // What the call showed the model, as data URLs. The output text only names
  // them, so without these a person is told a picture was taken and not shown it.
  images?: string[];
  // Video files the call handed to the model to watch natively: name and size,
  // never the bytes.
  videos?: { name: string; size: number; durationMs?: number; model: string }[];
  err?: string;
  // The host's dotted identity for a refusal. err is only its wording.
  refusalCode?: string;
  workspaceLease?: WorkspaceLease;
  // outputDiff marks a shell result whose whole output is a unified diff, so the
  // transcript renders it as a diff instead of flat text.
  outputDiff?: boolean;
  readOnly: boolean;
  // truncated is the pre-Bound projection kept for old journals; read bound.
  truncated?: boolean;
  bound?: Bound;
  durationMs?: number;
  // A sub-agent's estimated tokens per second on its status channel; not provider usage.
  rate?: number;
  contextTokens?: number;
  startedAt?: number;
  endedAt?: number;
  partial?: boolean;
  // Cumulative argument characters received so far on a partial dispatch — the
  // only liveness a streaming payload has before its JSON parses.
  argChars?: number;
  refreshed?: boolean;
  parentId?: string;
  // Who initiated this call. A separate axis from Event.source, which says who
  // produced the frame: an executor frame carries calls the model asked for,
  // calls the host minted for it, and calls the provider ran on its own side.
  issuer?: ToolIssuer;
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

// How much of the usage behind a quote carries a complete price fact. A single
// call is only ever complete or incomplete; an aggregate has two more answers,
// because it can price some of its calls and not others, and it can have billed
// nothing at all. Kept apart from displayStatus, which is about whether the
// requested currency could be shown, not about whether the cost is known.
export type CostCoverage = "none" | "complete" | "partial" | "incomplete";

export interface CostQuote {
  original: Money;
  selected?: Money;
  estimated: boolean;
  costComplete: boolean;
  // Absent from a kernel older than the field — a remote workspace can be one.
  coverage?: CostCoverage;
  // Why the cost fact is short, kept as provenance. The state is read from
  // coverage, never worked backwards out of this.
  incompleteReason?: string;
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
  // reason is the sentence the model was given; reasonCode is the same answer
  // as an identity, which is what this window renders.
  reason?: string;
  reasonCode?: string;
  fresh?: boolean;
  // Which answers beyond "once" this host will honour for this call.
  allowsSession?: boolean;
  allowsPersist?: boolean;
  kind?: "tool" | "plan" | "recovery";
  // What approving authorizes beyond the call, as the tool declares it; the
  // call's arguments ride along only then, because they are what it covers.
  scope?: string;
  input?: Record<string, unknown>;
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
  // The paired device the decision was made on; absent is the window.
  via?: Via;
}

// An image the host stored under .reasonix/attachments that a message references.
// Mirrors eventwire.Attachment: a client previews this, not how it was spelled.
export interface StoredAttachment {
  path: string;
}

// The paired device a person acted from. Mirrors eventwire.Via: the pairing's
// id, and the 设备 N the window listed it under at the time.
export interface Via {
  device: string;
  ordinal: number;
}

export interface AskOption {
  label: string;
  description?: string;
}

// No json tags on event.AskAnswer, so the wire keys are the Go field names.
export interface AskAnswer {
  QuestionID: string;
  Selected: string[];
  Value?: Record<string, unknown>;
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
  // Prefilled answer: a form sent back to be corrected keeps what was given.
  default?: string[];
  surface?: import("../ui/surface/spec").SurfaceSpec;
  callId?: string;
}

// Who is asking, when it is not the agent: an MCP server's form.
export interface AskOrigin {
  kind: "mcp";
  source: string;
  message?: string;
  note?: string;
  url?: string;
 urlHost?: string;
 urlLocal?: boolean;
}

export interface Ask {
  id: string;
  questions: AskQuestion[];
  origin?: AskOrigin;
  skipInMs?: number;
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
  kind: "text" | "markdown" | "row" | "stack" | "kv" | "meter" | "pip" | "button" | "divider" | "field" | "choice_card" | "editable_text" | "diff_preview" | "map" | "comparison" | "dashboard";
  value?: string;
  key?: string;
  label?: string;
  tone?: ExtensionViewTone;
  progress?: number;
  actionId?: string;
  children?: ExtensionViewNode[];
  field?: SurfaceField;
  diff?: SurfaceDiff;
  dataset?: SurfaceDataset;
}

export interface SurfaceDiff {
  before: string;
  after: string;
}

export interface SurfaceField {
  type: "string" | "number" | "bool" | "choice" | "multichoice";
  required?: boolean;
  default?: string | number | boolean | string[];
  min?: number;
  max?: number;
  max_length?: number;
  options?: SurfaceOption[];
}

export interface SurfaceOption {
  value: string;
  label: string;
  description?: string;
  preview?: string;
  group?: string;
  location?: SurfaceLocation;
  price?: number;
  currency?: string;
  facts?: SurfaceFact[];
  image?: SurfaceImage;
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
  code?: string;
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
/** The host's reading of whether the run still moves: rounds since the last
 *  effect it could observe, and input spent against the backstop. Advisory —
 *  only pausing, which the user switched on, ends the run. */
export interface ProgressWatch {
  stalled: boolean;
  cause?: "rounds" | "tokens" | "perseveration";
  idleRounds: number;
  roundLimit?: number;
  promptTokens?: number;
  tokenLimit?: number;
  tokenMultiple?: number;
  pausing?: boolean;
}

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
  // What the fold cost; coverageRequired counts the changes and failures it
  // folded and coverageMissing how many the digest did not carry.
  sourceTokens?: number;
  projectionTokens?: number;
  coverageRequired?: number;
  coverageMissing?: number;
  // The host wrote the dropped facts in itself, because the digest did not.
  coverageBackstopped?: boolean;
  // Which threshold sent this fold and how big it is.
  boundary?: string;
  triggerTokens?: number;
  // Why a fold that installed nothing did not; the host's class, not a sentence.
  code?: string;
  // What the prompt still holds when a fold left a large share of the window.
  remains?: ContextComposition;
}

export interface StreamAttempt {
  id: string;
  action: "begin" | "discard" | "commit";
  attempt?: number;
  max?: number;
  reason?: string;
  firstTokenMs?: number; // commit only: request sent to first output, host-measured
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
  // The kernel's grading of what was left unproven; absent when nothing was.
  severity?: "notice" | "warn";
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

export interface WireEvent {
  kind: Kind;
 nonPersistable?: boolean;
  text?: string;
  detail?: string;
  code?: string;
  reasoning?: string;
  // message: how long the kernel measured this reply thinking, in ms.
  thoughtMs?: number;
  // turn_done: ms the run took up to its final answer; absent when it gave none.
  workDurationMs?: number;
  // turn_done: unix ms the turn began; sent only alongside workDurationMs.
  workStartedAt?: number;
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
  progressWatch?: ProgressWatch;
  workspaceLease?: WorkspaceLease;
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
  // Which producer wrote this frame: "executor", "planner", "subagent". One
  // turn can carry two models, and the alternative is reading it off the last
  // phase marker — an adjacency the record does not even keep.
  source?: string;
  // Which model wrote this: the turn's on turn_started, a second model's on the
  // text it wrote. A reply with none is the turn's, and reading it off the
  // composer instead named the wrong one whenever the setting had moved on.
  modelRef?: string;
  // turn_started: which message this turn is about. authoredTurn numbers the
  // conversation's own turns, msgIndex the session log — a checkpoint's `turn`
  // is neither. A client mints its own user row, so this is the only thing
  // that says which row the kernel just started a turn for.
  authoredTurn?: number;
  msgIndex?: number;
  // turn_started / steer: the paired device the message came from; absent is
  // the window. The message itself rides text, for every client that did not
  // send it.
  via?: Via;
  // turn_started / steer: the stored attachments the message references.
  attachments?: StoredAttachment[];
  // turn_started: the agent or group the host ran this turn for.
  origin?: MessageOrigin;
  // steer: the host wrote this guidance, so it is never drawn as the person's.
  hostAuthored?: boolean;
  // recovery_paused is no longer emitted; sessions recorded before the retry
  // budgets were removed still carry it, so a reader has to render it.
  outcome?: "final_readiness" | "recovery_paused" | "no_progress";
  phase?: string;
  retryAttempt?: number;
  retryMax?: number;
  retryScope?: "headers" | "stream";
  retryCause?: "connection_closed" | "timeout" | "upstream_status" | "stream_idle" | "upstream_error";
  retryStatus?: number;
  retryDelayMs?: number;
  retryTimeoutSecs?: number;
  itemId?: string;
  // Set by the transport on frames that must not be missed, so a client can
  // tell it missed one. Streaming deltas carry none — losing one costs nothing
  // the next frame does not restate.
  seq?: number;
}

/** What the frames in a TrajectoryRead cover.
 *
 *  - complete: every frame the session produced, from its start to what is
 *    durable now. `events: []` under this answer means it produced none.
 *  - truncated: the frames are a trustworthy prefix and nothing more. What
 *    follows the last one is unknown — never "nothing happened".
 *  - not_recorded: nothing recorded this session, so there is no prefix at all.
 */
export type TrajectoryAvailability = "complete" | "truncated" | "not_recorded";

/** The frames, and what they cover. Coverage travels with them because it
 *  cannot be read off them: the last line of a log the host stopped extending
 *  looks exactly like the last line of a session that ended there. */
export interface TrajectoryRead {
  availability: TrajectoryAvailability;
  events: WireEvent[];
}
