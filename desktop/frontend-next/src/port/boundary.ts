// boundary.ts — the tool boundary as an editor sees it: which calls are
// refused before they run, and how far an approved one may write.

// The fine-grained gate. The three lists are checked in the order deny → ask →
// allow, and mode decides only what nothing matched. deny is the one entry no
// approval prompt can talk its way past, which is why it is worth a screen.
export interface PermissionLists {
  mode: string;
  allow: string[];
  ask: string[];
  deny: string[];
}

export interface DormantPermissionRule {
  list: string;
  rule: string;
  tool: string;
}

// Editable user lists and separate project rules that are in force. shadowedBy
// names a project config that outranks the edited file; effective is the
// merged boundary the controller actually loaded.
export interface PermissionRules extends PermissionLists {
  path: string;
  // What was allowed on a prompt for this session alone. Nothing wrote it down,
  // so the file on screen is less than the agent may currently do.
  granted?: string[];
  remembered?: string[];
  rememberedPath?: string;
  rememberedErrorCode?: string;
  shadowedBy?: string;
  effective?: PermissionLists;
  // Saved rules that name no tool, so they match nothing.
  dormant?: DormantPermissionRule[];
}

// Where an approved write may land, and whether bash runs jailed. The
// effective* fields are what the confiner will use: an empty workspaceRoot is
// not "anywhere", it is "the session directory", and an empty bash is not "off".
export interface SandboxSettings {
  bash: string;
  network: boolean;
  workspaceRoot: string;
  allowWrite: string[];
  // Refuse a whole-file write over a change made since the agent last saw the file.
  protectChangedFiles: boolean;
  effectiveWriteRoots: string[];
  // The mode that will actually run, which the configured one does not always
  // survive to: Windows has no OS backend and forces off, an unset value
  // enforces elsewhere, and a project file outranks this one.
  effectiveBash: string;
  // False where this host has no OS sandbox at all — enforce would then refuse
  // every bash call rather than run it unconfined, so the switch says so
  // instead of pretending to work.
  available: boolean;
  why?: string;
  // The same explanation's code, which is what has wording in this window.
  whyCode?: string;
  platform: string;
  path: string;
  shadowedBy?: string;
}

// The project's primary folder and the extra folders the user granted it write
// access to. pending counts other panes on the project that were busy and take
// the change at their next rebuild.
export interface WorkspaceRoots {
  primary: string;
  extra: string[];
  pending?: number;
}

// What a settings surface shows in place of a save it cannot perform. The
// kernel refuses every write while the file will not parse, so this arrives
// before anything is tried rather than as each panel's own error.
export interface ConfigProblem {
  path: string;
  line?: number;
  key?: string;
  excerpt?: string;
  // The offending line said the other way. Present only when the file parses
  // after the change, so it is an offer rather than a guess.
  repair?: string;
  // Which values are on screen instead: "last-known-good" or "defaults".
  recovered?: string;
  detail?: string;
}

export interface ConfigRepair {
  backup: string;
  problem: ConfigProblem | null;
}

// The built-in browser tools as the user file holds them, beside what this
// workspace will run with: a project file may set the same key and outrank it.
export interface BrowserToolsSettings {
  enabled: boolean;
  effective: boolean;
  path: string;
}

// How a sub-agent that declared no write_paths holds its claim, as the user
// file records it. A project file cannot set it, so there is one value.
export interface SubagentClaimsSettings {
  optimistic: boolean;
  path: string;
}

// How this machine's sessions hold the workspace write lease against one
// another, as the user file records it. A project file cannot set it. A running
// session keeps the lease it was built with, so a change reaches sessions
// opened after it.
export interface WriteLeaseSettings {
  optimistic: boolean;
  path: string;
}

// Whether a submitted plan starts executing without the approval click, as the
// user file records it. A project file cannot set it.
export interface PlanApprovalSettings {
  auto: boolean;
  path: string;
}

export interface PeerView {
  key: string;
  title: string;
  project?: string;
}

export interface PeerHeld {
  id: string;
  fromSession: string;
  fromTitle?: string;
  fromProject?: string;
  sentAt: string;
  body: string;
}

export type PeerThreadState = "waiting" | "working" | "replied" | "declined";

export interface PeerThread {
  key: string;
  title: string;
  project: string;
  sessionPath?: string;
  state: PeerThreadState;
  since: string;
  messageId: string;
}

export interface PeersState {
  enabled: boolean;
  peers: PeerView[];
  held: PeerHeld[];
  threads?: PeerThread[];
}

export interface PeerProject {
  path: string;
  name: string;
}

/** The session-messages switches as the kernel holds them (snake_case on the
 *  wire). Saved values reach the model only at the next turn boundary, so
 *  `pending` stays true until then. */
export interface PeersSettings {
  enabled: boolean;
  dispatch: boolean;
  release_projects: PeerProject[];
  path: string;
  applies: "next_turn";
  pending: boolean;
  // False where the session has no folder to open (a headless run): there is
  // nowhere for messages to be held, so the switches are inert.
  available: boolean;
}

export interface PeersSettingsEdit {
  enabled?: boolean;
  dispatch?: boolean;
  release_projects?: string[];
}

export type PeerRemember = "project" | "none";

export interface PeerReceipt {
  id: string;
  to: PeerView;
}

export interface AutoArchiveSettings {
  enabled: boolean;
  days: number;
  defaultDays: number;
  path: string;
}

// The user's system persona. The kernel serves the built-in starting points
// with the settings so the page never keeps a second copy of the prompt text.
export interface PersonaPreset {
  id: string;
  prefix: string;
  suffix: string;
  complete: boolean;
  include_runtime_context: boolean;
}

export interface PersonaSettings {
  prefix: string;
  suffix: string;
  complete: boolean;
  include_runtime_context: boolean;
  path: string;
  presets: PersonaPreset[];
}

/** The four fields a save writes; path and presets are the kernel's answer. */
export type PersonaDraft = Pick<PersonaSettings, "prefix" | "suffix" | "complete" | "include_runtime_context">;

export type DisplayCurrencyMode = "auto" | "CNY" | "USD";

// Which currency costs are shown in; the user file holds it.
export interface UnconvertedPrice {
  provider: string;
  model: string;
  currency: string;
}

export interface DisplayCurrencySettings {
  mode: DisplayCurrencyMode;
  path: string;
  // Models whose price cannot be shown in a pinned currency.
  unconverted: UnconvertedPrice[];
}

// Whether the changed-files pill is drawn above the composer; the user file
// holds it.
// Whether Studio folds a turn's intermediate work under one line; the user file
// holds it.
export interface FoldWorkSettings {
  fold: boolean;
  path: string;
}

// How much the model says between tool calls; the user file holds it.
export interface TurnNarrationSettings {
  level: "brief" | "detailed";
  path: string;
}

// Where web_search and web_fetch may send things. The lists are ordered; a paid
// backend is tried only after the ones before it failed, and one named in a
// paid-allowed list is used without asking each time.
export interface WebAccessSettings {
  search_backends: string[];
  searxng_url: string;
  search_paid_allowed: string[];
  fetch_backends: string[];
  fetch_paid_allowed: string[];
  known_search_backends: string[];
  known_fetch_backends: string[];
  paid_backends: string[];
  deepseek_api_ready: boolean;
  path: string;
}

export type WebAccessPatch = Partial<Pick<WebAccessSettings, "search_backends" | "searxng_url" | "search_paid_allowed" | "fetch_backends" | "fetch_paid_allowed">>;

export interface ChangedFilesSettings {
  show: boolean;
  path: string;
}

// When a run reads as no longer moving. Only the user file holds it: a
// project file cannot pause the user's runs.
// How long an ask question waits before the host skips it; 0 waits. The user
// file holds it, so the terminal reads the same answer.
export interface AskTimeoutSettings {
  seconds: number;
  // The kernel's code for a value the file holds that cannot be applied.
  invalid?: string;
  defaultSeconds: number;
  minSeconds: number;
  maxSeconds: number;
  path: string;
}

// Whether the end-of-turn delivery receipt is drawn as a card; the user file
// holds it, so the terminal reads the same answer.
export interface ReceiptSettings {
  show: boolean;
  path: string;
}

export interface ProgressWatchSettings {
  pause: boolean;
  rounds: number;
  tokenMultiple: number;
  defaultRounds: number;
  defaultTokenMultiple: number;
  path: string;
}
