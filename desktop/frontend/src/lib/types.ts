// Wire contract — mirrors desktop/wire.go (itself mirroring internal/serve/wire.go).
// One event channel carries every kind; `kind` discriminates the payload.

export type EventKind =
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
  | "approval_request"
  | "ask_request"
  | "turn_done"
  | "compaction_started"
  | "compaction_done"
  | "retrying";

export interface WireCompaction {
  trigger?: string; // "auto" | "manual"
  messages?: number; // done: how many messages were folded into the summary
  summary?: string; // done: the briefing (empty on an aborted pass)
  archive?: string; // done: archive path, if any
}

export interface WireTool {
  id?: string;
  name: string;
  args?: string;
  output?: string;
  err?: string;
  readOnly: boolean;
  truncated?: boolean;
  partial?: boolean; // an early dispatch (name only) — a full one with args follows
  parentId?: string; // set on a sub-agent's calls — the parent `task` call's id
}

export interface WireUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  reasoningTokens?: number;
  // Session-cumulative cache tokens — the status bar shows the aggregate
  // hit-rate (Σhit/Σ(hit+miss)), steadier than the single-turn cacheHitTokens.
  sessionCacheHitTokens: number;
  sessionCacheMissTokens: number;
  cost?: number;
  currency?: string;
  // Deprecated compatibility alias. Prefer cost + currency.
  costUsd?: number;
}

export interface WireApproval {
  id: string;
  tool: string;
  subject: string;
}

export interface WireAskOption {
  label: string;
  description?: string;
}

export interface WireAskQuestion {
  id: string;
  header?: string;
  prompt: string;
  options: WireAskOption[];
  multi?: boolean;
}

export interface WireAsk {
  id: string;
  questions: WireAskQuestion[];
}

// QuestionAnswer is the reply for one question, sent back via AnswerQuestion.
export interface QuestionAnswer {
  questionId: string;
  selected: string[];
}

export interface WireEvent {
  kind: EventKind;
  text?: string;
  reasoning?: string;
  level?: "info" | "warn";
  tool?: WireTool;
  usage?: WireUsage;
  approval?: WireApproval;
  ask?: WireAsk;
  compaction?: WireCompaction;
  err?: string;
  retryAttempt?: number;
  retryMax?: number;
  // Tab routing: set by the Go-side tabEventSink so multi-tab frontends
  // route each event to the correct per-tab reducer.
  tabId?: string;
  sessionHitTokens?: number;
  sessionMissTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  // Deprecated compatibility alias. Prefer sessionCost + sessionCurrency.
  sessionCostUsd?: number;
}

// Tab management types (desktop/tabs.go).
export interface TabMeta {
  id: string;
  tabType?: "session" | "file";
  scope: string;
  workspaceRoot: string;
  workspaceName: string;
  topicId: string;
  topicTitle: string;
  filePath?: string;
  projectColor?: string;
  label: string;
  ready: boolean;
  running: boolean;
  mode: Mode;
  startupErr?: string;
  active: boolean;
  cwd: string;
}

export interface ProjectNode {
  key: string;
  kind: "project" | "topic" | "global_folder" | "global_topic";
  label: string;
  root?: string;
  topicId?: string;
  projectColor?: string;
  turns?: number;
  lastActivityAt?: number;
  open?: boolean;
  running?: boolean;
  children?: ProjectNode[];
}

export interface TopicMeta {
  id: string;
  title: string;
  createdAt: number;
}

export interface ContextPanelInfo {
  usedTokens: number;
  windowTokens: number;
  promptTokens: number;
  completionTokens: number;
  reasoningTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  sessionCost?: number;
  sessionCurrency?: string;
  // Deprecated compatibility alias. Prefer sessionCost + sessionCurrency.
  sessionCostUsd?: number;
  readFiles: ReadFileRecord[];
  changedFiles: ChangedFileInfo[];
}

export interface ReadFileRecord {
  path: string;
  turn: number;
  time: number;
  offset?: number;
  limit?: number;
  truncated?: boolean;
}

export interface ChangedFileInfo {
  path: string;
  oldPath?: string;
  sources: string[];
  gitStatus?: string;
  turns: number[];
  latestPrompt?: string;
  latestTime?: number;
}

// Bound-method payloads (desktop/app.go).
export interface HistoryMessage {
  role: string;
  content: string;
  reasoning?: string;
}

// CheckpointMeta is one rewind point (a user turn) for the rewind UI.
export interface CheckpointMeta {
  turn: number;
  prompt: string;
  files: string[];
  time: number; // unix ms
  canCode?: boolean;
  canConversation?: boolean;
}

// SessionMeta is one saved session for the history panel.
export interface SessionMeta {
  path: string;
  preview: string;
  title?: string; // user-chosen name; falls back to preview when empty
  turns: number;
  createdAt: number; // unix milliseconds
  lastActivityAt: number; // unix milliseconds
  modTime: number; // compatibility alias for lastActivityAt
  deletedAt?: number; // unix milliseconds, present for trashed sessions
  current: boolean;
  open: boolean;
  scope?: string;       // "project" | "global"; empty for legacy → treated as "global"
  workspaceRoot?: string;
  topicId?: string;
  topicTitle?: string;
}

export interface WorkspaceView {
  path: string;
  name: string;
  current: boolean;
}

export interface ContextInfo {
  used: number;
  window: number;
  compactRatio?: number;
}

export interface Meta {
  label: string;
  ready: boolean;
  startupErr?: string;
  eventChannel: string;
  cwd: string;
  bypass?: boolean; // YOLO mode on (auto-approve every tool call)
}

// Mode is the input mode cycled by Shift+Tab: normal (shown as auto) → plan
// (read-only) → yolo (auto-approve every tool call; deny rules still apply).
export type Mode = "normal" | "plan" | "yolo";

export interface CommandInfo {
  name: string; // without the leading slash
  description: string;
  hint?: string;
  kind: "builtin" | "custom" | "mcp" | "skill";
}

export interface DirEntry {
  name: string;
  isDir: boolean;
}

export interface DroppedItem {
  kind: "workspace" | "attachment";
  path: string;
  isDir?: boolean;
  previewUrl?: string;
}

export interface FilePreview {
  path: string;
  body: string;
  size: number;
  truncated: boolean;
  binary: boolean;
  err?: string;
}

export interface WorkspaceChangeView {
  path: string;
  oldPath?: string;
  sources: string[];
  gitStatus?: string;
  turns?: number[];
  latestPrompt?: string;
  latestTime?: number;
}

export interface WorkspaceChangesView {
  files: WorkspaceChangeView[];
  gitAvailable: boolean;
  gitErr?: string;
}

export interface ComposerInsertRequest {
  id: number;
  text: string;
}

// MCP & Skills drawer (desktop/app.go Capabilities) — the GUI counterpart to
// /mcp + /skill: connected/failed servers and discoverable skills.
export interface ServerView {
  name: string;
  transport: string;
  status: "connected" | "deferred" | "failed" | "initializing" | "disabled";
  builtIn?: boolean;
  configured?: boolean;
  autoStart: boolean;
  tier?: "lazy" | "background" | "eager" | string;
  command?: string;
  args?: string[];
  url?: string;
  envKeys?: string[];
  tools: number;
  prompts: number;
  resources: number;
  error?: string;
  toolList?: MCPToolView[];
  authStatus?: "none" | "possible" | "required" | string;
  authUrl?: string;
  authConfigured?: boolean;
}
export interface MCPToolView {
  name: string;
  description: string;
}
export interface SkillView {
  name: string;
  description: string;
  scope: string;
  runAs: string;
  enabled: boolean;
}
export interface SkillRootSkillView {
  name: string;
  description: string;
  scope: string;
  runAs: string;
}
export interface SkillRootView {
  dir: string;
  scope: string;
  priority: number;
  status: string;
  configured: boolean;
  skills: number;
  skillItems?: SkillRootSkillView[];
  warning?: string;
}
export interface CapabilitiesView {
  servers: ServerView[];
  skills: SkillView[];
  skillRoots: SkillRootView[];
}
export interface MCPServerInput {
  name: string;
  transport: string; // stdio | http | sse
  command: string;
  args: string[];
  url: string;
  env?: Record<string, string> | null;
  tier: string;
}

export interface ModelInfo {
  ref: string; // "provider/model" — pass to SetModel
  provider: string;
  model: string;
  current: boolean;
}

export interface EffortInfo {
  supported: boolean;
  current: string; // "auto" | "low" | "medium" | "high" | "xhigh" | "max"
  default: string;
  levels: string[];
}

// Slash sub-command / argument completion (desktop/app.go SlashArgs). Mirrors the
// CLI's arg hints so the composer can suggest e.g. /skill → list/show/new/paths.
export interface SlashArgItem {
  label: string;
  insert: string; // token to place at the current position
  hint: string;
  descend: boolean; // re-open the menu one level deeper after accepting
}
export interface SlashArgsResult {
  items: SlashArgItem[];
  from: number; // byte offset where the current token begins
}

// Memory panel payloads (desktop/app.go MemoryView).
export interface MemoryDoc {
  path: string;
  scope: string; // "user" | "ancestor" | "project" | "local"
  body: string;
}

export interface MemoryFact {
  name: string;
  title?: string;
  description: string;
  type: string; // "user" | "feedback" | "project" | "reference"
  body: string;
}

export interface MemoryScope {
  scope: string; // "user" | "project" | "local"
  path: string;
}

export interface MemoryView {
  docs: MemoryDoc[];
  facts: MemoryFact[];
  scopes: MemoryScope[];
  storeDir: string;
  available: boolean;
}

// Settings panel payloads (desktop/settings_app.go).
export interface ProviderView {
  name: string;
  kind: string;
  baseUrl: string;
  models: string[];
  default: string;
  apiKeyEnv: string;
  keySet: boolean; // the env var currently resolves to a value
  balanceUrl: string; // optional wallet-balance endpoint; "" disables the readout
  contextWindow: number;
  supportedEfforts: string[]; // custom /effort levels; empty = use built-in Kind/BaseURL default
  defaultEffort: string; // /effort level when user picks "auto" or unset; "" = supportedEfforts[0]
}

// BalanceInfo is the wallet-balance readout (desktop/app.go Balance). available
// is false when the provider declares no balanceUrl or a fetch failed; display is
// the formatted amount (e.g. "¥110.00").
export interface BalanceInfo {
  available: boolean;
  display: string;
  err?: string;
}

// JobView is one running background job (desktop/app.go Jobs) for the status bar.
export interface JobView {
  id: string;
  kind: string; // "bash" | "task"
  label: string;
  status: string; // "running"
  startedAt: number; // unix milliseconds
}

export interface PermissionsView {
  mode: string; // "ask" | "allow" | "deny"
  allow: string[];
  ask: string[];
  deny: string[];
}

export interface SandboxView {
  bash: string; // "enforce" | "off"
  network: boolean;
  workspaceRoot: string;
  allowWrite: string[];
}

export interface NetworkProxyView {
  type: string;
  server: string;
  port: number;
  username: string;
  password: string;
}

export interface NetworkView {
  proxyMode: string; // "auto" | "custom" | "off" (backend may still return legacy "env")
  proxyUrl: string;
  noProxy: string;
  proxy: NetworkProxyView;
}

export interface AgentView {
  temperature: number;
  maxSteps: number;
  systemPrompt: string;
}

export interface SettingsView {
  defaultModel: string;
  plannerModel: string;
  autoPlan: string;
  providers: ProviderView[];
  permissions: PermissionsView;
  sandbox: SandboxView;
  network: NetworkView;
  agent: AgentView;
  desktopLanguage: string; // "" | "en" | "zh"; empty = auto
  desktopTheme: string; // "auto" | "dark" | "light"
  desktopThemeStyle: string;
  closeBehavior: string; // "background" | "quit"
  configPath: string;
  providerKinds: string[]; // provider implementations the kernel registered (for the kind picker)
  bypass: boolean; // live YOLO state (runtime-only) — whether approvals are skipped this session
}

// Auto-updater payloads (desktop/updater.go). UpdateInfo drives the update banner;
// UpdateProgress streams on the "updater:progress" event during ApplyUpdate.
export interface UpdateInfo {
  available: boolean;
  current: string;
  latest: string;
  notes: string;
  canSelfUpdate: boolean; // win/linux true; macOS false (no cert → manual download)
  downloadUrl: string; // human-facing releases page (macOS path / fallback link)
  assetSize: number; // running platform's artifact size, for the progress bar
  err?: string; // set when the check itself failed (both endpoints down)
}

export interface UpdateProgress {
  phase: "downloading" | "verifying" | "applying" | "done" | "error";
  received: number;
  total: number;
  err?: string;
}
