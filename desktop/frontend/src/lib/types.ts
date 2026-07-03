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
  | "mcp_surface_ready"
  | "retrying"
  | "steer"
  | "memory_compiler_stats"
  | "guardian_assessment";

export interface WireCompaction {
  trigger?: string; // "auto" | "manual"
  messages?: number; // done: how many messages were folded into the summary
  summary?: string; // done: the briefing (empty on an aborted pass)
  archive?: string; // done: archive path, if any
}

export interface WireProfile {
  model?: string;
  effort?: string;
}

export interface WireTool {
  id?: string;
  name: string;
  args?: string;
  output?: string;
  err?: string;
  readOnly: boolean;
  truncated?: boolean;
  durationMs?: number;
  partial?: boolean; // an early dispatch (name only) — a full one with args follows
  parentId?: string; // set on a sub-agent's calls — the parent `task` call's id
  diff?: string;
  added?: number;
  removed?: number;
  profile?: WireProfile; // subagent model/effort resolved for this call
}

export interface WireCacheDiagnostics {
  prefixHash: string;
  prefixChanged: boolean;
  prefixChangeReasons?: string[];
  systemHash: string;
  toolsHash: string;
  logRewriteVersion: number;
  toolSchemaTokens: number;
  cacheMissTokens: number;
  cacheHitTokens: number;
}

export interface WireUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  reasoningTokens?: number;
  source?: string;
  cacheDiagnostics?: WireCacheDiagnostics;
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
  reason?: string;
}

export interface WireGuardian {
  id: string;
  tool: string;
  subject: string;
  outcome: string;
  risk_level?: string;
  user_authorization?: string;
  rationale?: string;
  duration_ms?: number;
  usage?: WireUsage;
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

export interface MemoryCitation {
  id?: string;
  source: string;
  lineStart?: number;
  lineEnd?: number;
  note?: string;
  kind?: string;
}

export interface MemoryCompilerStats {
  injected: boolean;
  usefulIR: boolean;
  compiledTokens: number;
  irOverheadTokens: number;
  memoryReferences: number;
  constraints: number;
  riskNotes: number;
  executionSteps: number;
  totalNodes: number;
  highSignalNodes: number;
  toolResultNodes: number;
  decisionNodes: number;
  strategyCount: number;
  learningCount: number;
}

export interface WireEvent {
  kind: EventKind;
  text?: string;
  reasoning?: string;
  memoryCitations?: MemoryCitation[];
  memoryCompiler?: MemoryCompilerStats;
  level?: "info" | "warn";
  tool?: WireTool;
  usage?: WireUsage;
  approval?: WireApproval;
  ask?: WireAsk;
  compaction?: WireCompaction;
  guardian?: WireGuardian;
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
  workspacePath?: string;
  gitBranch?: string;
  topicId: string;
  topicTitle: string;
  sessionPath?: string;
  readOnly?: boolean;
  filePath?: string;
  projectColor?: string;
  label: string;
  ready: boolean;
  running: boolean;
  pendingPrompt?: boolean;
  backgroundJobs?: number;
  cancelRequested?: boolean;
  cancellable?: boolean;
  mode: Mode;
  collaborationMode?: CollaborationMode;
  toolApprovalMode?: ToolApprovalMode;
  tokenMode?: TokenMode;
  goal?: string;
  goalStatus?: GoalStatus;
  autoResearch?: AutoResearchCompactView;
  recovered?: boolean;
  recoveryReason?: string;
  recoveryDigest?: string;
  recoveryParentId?: string;
  startupErr?: string;
  active: boolean;
  cwd: string;
}

export interface ProjectNode {
  key: string;
  kind: "project" | "topic" | "session" | "global_folder" | "global_topic" | "global_session";
  label: string;
  root?: string;
  topicId?: string;
  sessionPath?: string;
  projectColor?: string;
  turns?: number;
  createdAt?: number;
  lastActivityAt?: number;
  open?: boolean;
  running?: boolean;
  status?: ProjectTopicStatus;
  pinned?: boolean;
  recovered?: boolean;
  recoveryReason?: string;
  recoveryDigest?: string;
  recoveryParentId?: string;
  children?: ProjectNode[];
}

export type ProjectTopicStatus = "thinking" | "streaming" | "waiting_confirmation" | "background_job" | "paused" | "error";

export interface TopicMeta {
  id: string;
  title: string;
  createdAt: number;
}

export interface SessionRecoveryEvent {
  originalPath?: string;
  recoveryPath: string;
  scope?: string;
  workspaceRoot?: string;
  topicId?: string;
  topicTitle?: string;
  recoveryReason?: string;
  recoveryDigest?: string;
  recoveryParentId?: string;
  existing?: boolean;
}

export interface SessionRecoveryFailedEvent {
  reason?: "lease_held" | "lease_unavailable" | string;
}

export interface ContextPanelInfo {
  usedTokens: number;
  windowTokens: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  reasoningTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  sessionCacheHitTokens: number;
  sessionCacheMissTokens: number;
  sessionCompletionTokens: number;
  requestCount?: number;
  elapsedMs?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  // Deprecated compatibility alias. Prefer sessionCost + sessionCurrency.
  sessionCostUsd?: number;
  sources?: Record<string, UsageSourceStats>;
  mock?: boolean;
  readFiles: ReadFileRecord[];
  changedFiles: ChangedFileInfo[];
}

export interface UsageSourceStats {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  reasoningTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  requestCount: number;
  sessionCost?: number;
  sessionCurrency?: string;
  sessionCostUsd?: number;
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
  submitText?: string;
  checkpointTurn?: number;
  createdAt?: number;
  reasoning?: string;
  memoryCitations?: MemoryCitation[];
  level?: "info" | "warn";
  toolCalls?: HistoryToolCall[];
  toolCallId?: string;
  toolName?: string;
  toolResultArchived?: boolean;
  toolResultError?: string;
  pending?: boolean;
  trigger?: string;
  messages?: number;
  summary?: string;
  archive?: string;
}

export interface HistoryToolCall {
  id: string;
  name: string;
  arguments: string;
  subject?: string;
  summary?: string;
  diff?: string;
  added?: number;
  removed?: number;
  argumentsArchived?: boolean;
}

export interface HistoryPage {
  messages: HistoryMessage[];
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
}

export interface PromptHistoryEntry {
  text: string;
  at: number;          // unix ms
  sessionPath: string;
  turn: number;
}

export interface PromptHistoryResult {
  entries: PromptHistoryEntry[] | null;
  nonce: string;
  olderCursor?: string;
  hasOlder?: boolean;
}

// CheckpointMeta is one rewind point (a user turn) for the rewind UI.
export interface CheckpointMeta {
  turn: number;
  prompt: string;
  files: string[];
  fileCount?: number;
  filesTruncated?: boolean;
  turnFileCount?: number;
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
  kind?: "session" | "channel" | string;
  channel?: string;
  channelLabel?: string;
  remoteId?: string;
  chatType?: string;
  userId?: string;
  threadId?: string;
  sessionSource?: string;
}

// SessionReference is a session selected via @ past:chats for context injection.
export interface SessionReference {
  path: string;
  title: string;
  preview?: string;
  turns?: number;
  createdAt?: number;
  lastActivityAt?: number;
}

export interface WorkspaceView {
  path: string;
  name: string;
  current: boolean;
}

export interface ContextInfo {
  used: number;
  window: number;
  sessionTokens: number;
  compactRatio?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  cacheHitTokens?: number;
  cacheMissTokens?: number;
  sources?: Record<string, UsageSourceStats>;
}

export interface Meta {
  label: string;
  ready: boolean;
  startupErr?: string;
  eventChannel: string;
  cwd: string;
  workspaceRoot?: string;
  workspaceName?: string;
  workspacePath?: string;
  sessionPath?: string;
  gitBranch?: string;
  imageInputEnabled?: boolean;
  autoApproveTools?: boolean;
  bypass?: boolean; // legacy JSON key for YOLO/full-access tool auto-approval
  collaborationMode?: CollaborationMode;
  toolApprovalMode?: ToolApprovalMode;
  tokenMode?: TokenMode;
  goal?: string;
  goalStatus?: GoalStatus;
  autoResearch?: AutoResearchCompactView;
}

export type CollaborationMode = "normal" | "plan" | "goal";
export type ToolApprovalMode = "ask" | "auto" | "yolo";
export type TokenMode = "full" | "economy";
export type GoalStatus = "running" | "complete" | "blocked" | "stopped";

export interface AutoResearchCompactView {
  taskId: string;
  status: "running" | "blocked" | "complete" | "stopped" | "invalid";
  iteration: number;
  pivotRequired: boolean;
  staleCount: number;
}

export interface AutoResearchCriterionView {
  id: string;
  description: string;
  required: boolean;
  evidenceCount: number;
  status: string;
}

export interface AutoResearchStatusView extends AutoResearchCompactView {
  goal: string;
  currentDirection: string;
  pivotCount: number;
  lastHeartbeatAt: string;
  findingCount: number;
  openCriteria: AutoResearchCriterionView[];
  blocker: string;
  taskPath: string;
  nextRequiredAction: string;
}

export interface AutoResearchFindingView {
  id: string;
  kind: string;
  summary: string;
  source: string;
  command?: string;
  paths?: string[];
  accepted: boolean;
  createdAt: string;
}

export interface AutoResearchEvidenceView {
  id: string;
  kind: string;
  summary: string;
  source: string;
  command?: string;
  paths?: string[];
  accepted: boolean;
}

export function normalizeCollaborationMode(mode?: string, goal?: string, legacyMode?: Mode): CollaborationMode {
  if (mode === "plan" || mode === "goal" || mode === "normal") return mode;
  if (legacyMode && modeHasPlan(legacyMode)) return "plan";
  if ((goal ?? "").trim()) return "goal";
  return "normal";
}

export function normalizeToolApprovalMode(
  mode?: string,
  legacyMode?: Mode,
  legacyAutoApproveTools?: boolean,
  fallbackMode?: ToolApprovalMode,
): ToolApprovalMode {
  const normalized = typeof mode === "string" ? mode.trim().toLowerCase() : "";
  if (normalized === "auto" || normalized === "yolo" || normalized === "ask") return normalized as ToolApprovalMode;
  if (legacyAutoApproveTools || (legacyMode && modeHasAutoApproveTools(legacyMode))) return "yolo";
  if (fallbackMode === "auto" && normalized === "") return "auto";
  return "ask";
}

export function normalizeTokenMode(mode?: string): TokenMode {
  if (mode === "economy") return "economy";
  return "full";
}

// Mode is the compatibility string for two independent composer axes:
// plan (read-only/user-plan gate) and yolo/full access (tool auto-approval).
export type Mode = "normal" | "plan" | "yolo" | "plan-yolo";

export function normalizeMode(mode?: string): Mode {
  if (mode === "plan" || mode === "yolo" || mode === "plan-yolo" || mode === "yolo-plan") {
    return mode === "yolo-plan" ? "plan-yolo" : mode;
  }
  return "normal";
}

export function modeHasPlan(mode: Mode): boolean {
  return mode === "plan" || mode === "plan-yolo";
}

export function modeHasAutoApproveTools(mode: Mode): boolean {
  return mode === "yolo" || mode === "plan-yolo";
}

export function modeFromAxes(plan: boolean, autoApproveTools: boolean): Mode {
  if (plan && autoApproveTools) return "plan-yolo";
  if (plan) return "plan";
  if (autoApproveTools) return "yolo";
  return "normal";
}

export function modeWithPlan(mode: Mode, plan: boolean): Mode {
  return modeFromAxes(plan, modeHasAutoApproveTools(mode));
}

export function modeWithAutoApproveTools(mode: Mode, autoApproveTools: boolean): Mode {
  return modeFromAxes(modeHasPlan(mode), autoApproveTools);
}

export interface CommandInfo {
  name: string; // without the leading slash
  description: string;
  hint?: string;
  kind: "builtin" | "custom" | "mcp" | "skill";
}

export interface DirEntry {
  name: string;
  path?: string;
  isDir: boolean;
  displayName?: string;
  displayPath?: string;
}

export interface DroppedItem {
  kind: "workspace" | "attachment";
  path: string;
  isDir?: boolean;
  displayPath?: string;
  previewUrl?: string;
}

export interface FilePreview {
  path: string;
  body: string;
  size: number;
  truncated: boolean;
  binary: boolean;
  kind?: "image" | "pdf";
  mime?: string;
  url?: string;
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
  gitBranch?: string;
}

export interface GitCommitView {
  hash: string;
  author: string;
  date: string;
  message: string;
}

export interface GitCommitDetailView {
  diff?: string;
  files?: string[];
}

export interface ComposerInsertRequest {
  id: number;
  text: string;
  mode?: "insert" | "replace";
}

// MCP & Skills drawer (desktop/app.go Capabilities) — the GUI counterpart to
// /mcp + /skill: connected/failed servers and discoverable skills.
export interface ServerView {
  name: string;
  transport: string;
  status: "connected" | "deferred" | "failed" | "initializing" | "disabled";
  startIntent?: "off" | "automatic" | string;
  runtimeState?: "idle" | "connecting" | "ready" | "issue" | string;
  builtIn?: boolean;
  configured?: boolean;
  autoStart: boolean;
  tier?: "background" | "eager" | string;
  command?: string;
  args?: string[];
  url?: string;
  envKeys?: string[];
  headerKeys?: string[];
  tools: number;
  prompts: number;
  resources: number;
  error?: string;
  toolList?: MCPToolView[];
  trustedReadOnlyTools?: string[];
  authStatus?: "none" | "possible" | "required" | string;
  authUrl?: string;
  authConfigured?: boolean;
}
export interface MCPToolView {
  name: string;
  description: string;
  readOnlyHint?: boolean;
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
  removable: boolean;
  skills: number;
  skillItems?: SkillRootSkillView[];
  warning?: string;
}
export interface CapabilitiesView {
  servers: ServerView[];
  skills: SkillView[];
  skillRoots: SkillRootView[];
  plugins: PluginView[];
}
export interface SkillsSettingsView {
  skills: SkillView[];
  skillRoots: SkillRootView[];
}
export interface PluginView {
  name: string;
  version?: string;
  description?: string;
  source?: string;
  root: string;
  manifestKind?: string;
  enabled: boolean;
  skills: number;
  hooks: number;
  mcpServers: number;
  skillDetails?: PluginSkillView[];
  hookDetails?: PluginHookView[];
  mcpServerDetails?: PluginMCPServerView[];
  warnings?: string[];
  error?: string;
}
export interface PluginSkillView {
  name: string;
  description?: string;
  path?: string;
  invocation?: string;
  runAs?: string;
}
export interface PluginHookView {
  event: string;
  match?: string;
  command?: string;
  contextFile?: string;
  description?: string;
}
export interface PluginMCPServerView {
  name: string;
  transport?: string;
  command?: string;
  url?: string;
}
export interface PluginInstallOptions {
  dryRun?: boolean;
  link?: boolean;
  replace?: boolean;
  name?: string;
}
export interface MCPServerInput {
  name: string;
  transport: string; // stdio | http | sse
  command: string;
  args: string[];
  url: string;
  env?: Record<string, string> | null;
  headers?: Record<string, string> | null;
  trustedReadOnlyTools?: string[];
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

export interface MemoryArchive extends MemoryFact {
  path: string;
  archivedAt?: string;
}

export interface MemoryScope {
  scope: string; // "user" | "project" | "local"
  path: string;
}

export interface MemorySuggestion {
  id: string;
  name: string;
  title: string;
  description: string;
  type: string;
  body: string;
  reason: string;
  evidence: string[];
}

export interface SkillSuggestion {
  id: string;
  name: string;
  description: string;
  scope: string;
  body: string;
  reason: string;
  evidence: string[];
}

export interface MemorySuggestionsView {
  memories: MemorySuggestion[];
  skills: SkillSuggestion[];
  generatedAt: string;
  available: boolean;
  source: string;
}

export interface MemoryView {
  docs: MemoryDoc[];
  facts: MemoryFact[];
  archives: MemoryArchive[];
  scopes: MemoryScope[];
  storeDir: string;
  storeGlobalDir?: string;
  available: boolean;
}

// SettingsTab is the top-level navigation item in the Settings Centre modal.
export type SettingsTab = "general" | "models" | "providers" | "bots" | "mcp" | "skills" | "plugins" | "memory" | "hooks" | "shortcuts" | "permissions" | "sandbox" | "network" | "appearance" | "updates";

// Settings panel payloads (desktop/settings_app.go).
export interface ProviderView {
  name: string;
  builtIn: boolean;
  added: boolean;
  kind: string;
  baseUrl: string;
  chatUrl?: string; // optional full chat completions URL; empty derives from baseUrl
  models: string[];
  visionModels: string[]; // subset of models that accepts image input
  visionModelsConfigured: boolean; // true when an empty list is an explicit choice
  modelsUrl: string; // optional override for model discovery; empty derives from baseUrl
  default: string;
  apiKeyEnv: string;
  headers?: Record<string, string> | null; // optional extra request headers for compatible gateways
  extraBody?: Record<string, unknown> | null; // optional extra top-level request body fields for compatible gateways
  keySet: boolean; // the env var currently resolves to a value
  requiresKey?: boolean; // false for explicit no-auth providers
  configured?: boolean; // selectable: key is set or no key is required
  keySource?: string;
  keySourcePath?: string;
  balanceUrl: string; // optional wallet-balance endpoint; "" disables the readout
  contextWindow: number;
  reasoningProtocol: string; // auto|deepseek|openai|none; empty = auto/model registry
  thinking: string; // provider-specific thinking override: ""|enabled|disabled|adaptive
  supportedEfforts: string[]; // custom /effort levels; empty = use built-in Kind/BaseURL default
  defaultEffort: string; // /effort level when user picks "auto" or unset; "" = supportedEfforts[0]
  modelOverrides?: ProviderModelOverrideView[] | null;
}

export interface ProviderModelOverrideView {
  model: string;
  reasoningProtocol: string;
  supportedEfforts: string[];
  defaultEffort: string;
  vision?: boolean | null;
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
  effectiveWorkspaceRoot: string;
  effectiveWriteRoots: string[];
  shell: string; // "auto" | "bash" | "powershell" | "pwsh"
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
  plannerMaxSteps: number;
  maxSubagentDepth: number;
  systemPrompt: string;
  coldResumePrune: boolean;
  reasoningLanguage: string; // "auto" | "zh" | "en"
}

export interface BotAllowlistView {
  enabled: boolean;
  allowAll: boolean;
  qqUsers: string[];
  feishuUsers: string[];
  weixinUsers: string[];
  qqGroups: string[];
  feishuGroups: string[];
  weixinGroups: string[];
}

export interface QQBotView {
  enabled: boolean;
  appId: string;
  appSecretEnv: string;
  secretSet: boolean;
  sandbox: boolean;
}

export interface FeishuBotView {
  enabled: boolean;
  domain: string;
  appId: string;
  appSecretEnv: string;
  secretSet: boolean;
  verificationToken: string;
  mode: string;
  webhookPort: number;
  requireMention: boolean;
}

export interface WeixinBotView {
  enabled: boolean;
  accountId: string;
  tokenEnv: string;
  tokenSet: boolean;
  apiBase: string;
}

export interface BotConnectionCredentialView {
  appId: string;
  appSecretEnv: string;
  accountId: string;
  tokenEnv: string;
  secretSet: boolean;
}

export interface BotConnectionSessionMappingView {
  remoteId: string;
  sessionId: string;
  sessionSource: string;
  chatType: string;
  userId: string;
  threadId: string;
  scope: "global" | "project" | string;
  workspaceRoot: string;
  updatedAt: string;
}

export interface BotConnectionView {
  id: string;
  provider: "qq" | "feishu" | "weixin" | string;
  domain: "qq" | "feishu" | "lark" | "weixin" | string;
  label: string;
  enabled: boolean;
  status: "disconnected" | "pending" | "connected" | "error" | string;
  model: string;
  toolApprovalMode: ToolApprovalMode | "" | string;
  workspaceRoot: string;
  credential: BotConnectionCredentialView;
  sessionMappings: BotConnectionSessionMappingView[];
  lastError: string;
  createdAt: string;
  updatedAt: string;
}

export interface BotSettingsView {
  enabled: boolean;
  model: string;
  toolApprovalMode: ToolApprovalMode | "" | string;
  maxSteps: number;
  debounceMs: number;
  allowlist: BotAllowlistView;
  qq: QQBotView;
  feishu: FeishuBotView;
  weixin: WeixinBotView;
  connections: BotConnectionView[];
}

export interface BotRuntimeStatusView {
  running: boolean;
  status: string;
  message: string;
  connections: number;
  startedAt: string;
}

export interface BotInstallStartResult {
  ok: boolean;
  provider: string;
  domain: string;
  installId: string;
  url: string;
  deviceCode: string;
  userCode: string;
  interval: number;
  expireIn: number;
  message: string;
}

export interface BotInstallPollResult {
  done: boolean;
  connection: BotConnectionView;
  status: string;
  message: string;
  error: string;
}

export interface HookConfigView {
  event: string;
  match?: string;
  command: string;
  description?: string;
  timeout?: number;
  cwd?: string;
}

export interface HooksSettingsView {
  scope: string;
  path: string;
  projectRoot: string;
  trusted: boolean;
  hooks: HookConfigView[];
  events: string[];
}

export interface BotConnectionDiagnostic {
  id: string;
  label: string;
  status: string;
  message: string;
  messageId: string;
  phase: string;
  code: string;
  reportKind: string;
  reportDetail: string;
  occurredAt: string;
}

export interface SettingsView {
  defaultModel: string;
  plannerModel: string;
  subagentModel: string;
  subagentEffort: string;
  autoPlan: string;
  providers: ProviderView[];
  officialProviders: ProviderView[];
  permissions: PermissionsView;
  sandbox: SandboxView;
  network: NetworkView;
  agent: AgentView;
  bot: BotSettingsView;
  desktopLanguage: string; // "" | "en" | "zh"; empty = auto
  desktopLayoutStyle: string; // "classic" | "workbench" | "creation"
  desktopTheme: string; // "auto" | "dark" | "light"
  desktopThemeStyle: string;
  closeBehavior: string; // "background" | "quit"
  displayMode: string;   // "standard" | "compact"
  statusBarStyle: string; // "icon" | "text"
  statusBarItems: string[]; // ordered visible status bar item ids
  defaultToolApprovalMode: ToolApprovalMode | string; // default for newly-created sessions
  checkUpdates: boolean; // check for new versions on startup
  telemetry: boolean; // anonymous launch ping (install id + version + OS)
  metrics: boolean; // aggregate desktop metrics (anonymous signal/bucket counts)
  memoryCompilerEnabled: boolean; // Memory v5 execution compiler
  configPath: string;
  providerKinds: string[]; // provider implementations the kernel registered (for the kind picker)
  autoApproveTools: boolean;
  bypass: boolean; // legacy JSON key for live YOLO/full-access tool auto-approval
}

export interface DesktopStartupSettingsView {
  bot: BotSettingsView;
  desktopLanguage: string; // "" | "en" | "zh"; empty = auto
  desktopLayoutStyle: string; // "classic" | "workbench"
  desktopTheme: string; // "auto" | "dark" | "light"
  desktopThemeStyle: string;
  displayMode: string;   // "standard" | "compact"
  statusBarStyle: string; // "icon" | "text"
  statusBarItems: string[]; // ordered visible status bar item ids
  checkUpdates: boolean; // check for new versions on startup
}

// Auto-updater payloads (desktop/updater.go). UpdateInfo drives the update banner;
// UpdateProgress streams on the "updater:progress" event during download/install.
export interface UpdateInfo {
  available: boolean;
  current: string;
  latest: string;
  notes: string;
  channel: string;
  canSelfUpdate: boolean; // macOS true only for signed/notarized builds
  manualOnly?: boolean;
  manualReason?: string;
  downloaded: boolean;
  downloadUrl: string; // human-facing releases page (macOS path / fallback link)
  assetSize: number; // running platform's artifact size, for the progress bar
  err?: string; // set when the check itself failed (both endpoints down)
}

export interface UpdateDownloadResult {
  version: string;
  channel: string;
  path: string;
  size: number;
  sha256: string;
}

export interface UpdateProgress {
  phase: "downloading" | "verifying" | "downloaded" | "installing" | "done" | "error";
  received: number;
  total: number;
  err?: string;
}
