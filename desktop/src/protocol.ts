export type ReadyEvent = { type: "$ready" };
export type ProtocolErrorEvent = { type: "$error"; message: string };
export type TurnCompleteEvent = { type: "$turn_complete" };
export type PathAccessRequiredEvent = {
  type: "$path_access_required";
  id: number;
  path: string;
  intent: "read" | "write";
  toolName: string;
  sandboxRoot: string;
  allowPrefix: string;
  prompt?: import("@reasonix/core-utils").ApprovalPrompt;
};

export type ConfirmRequiredEvent = {
  type: "$confirm_required";
  id: number;
  kind: "run_command" | "run_background";
  command: string;
  prompt?: import("@reasonix/core-utils").ApprovalPrompt;
};

export type ConfirmationChoice =
  | { type: "deny"; denyContext?: string }
  | { type: "run_once" }
  | { type: "always_allow"; prefix: string };

export type ChoiceOption = {
  id: string;
  title: string;
  summary?: string;
};

export type ChoiceRequiredEvent = {
  type: "$choice_required";
  id: number;
  question: string;
  options: ChoiceOption[];
  allowCustom: boolean;
};

export type ChoiceVerdict =
  | { type: "pick"; optionId: string }
  | { type: "text"; text: string }
  | { type: "cancel" };

export type PlanRequiredEvent = {
  type: "$plan_required";
  id: number;
  plan: string;
  steps?: unknown[];
  summary?: string;
};

export type PlanVerdict =
  | { type: "approve"; feedback?: string }
  | { type: "refine"; feedback?: string }
  | { type: "cancel"; feedback?: string };

export type PlanStep = {
  id: string;
  title: string;
  action: string;
  risk?: "low" | "med" | "high";
};

export type CheckpointRequiredEvent = {
  type: "$checkpoint_required";
  id: number;
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
  completed: number;
  total: number;
};

export type CheckpointVerdict =
  | { type: "continue" }
  | { type: "revise"; feedback?: string }
  | { type: "stop" };

export type RevisionRequiredEvent = {
  type: "$revision_required";
  id: number;
  reason: string;
  remainingSteps: PlanStep[];
  summary?: string;
};

export type RevisionVerdict = { type: "accepted" } | { type: "rejected" } | { type: "cancelled" };

export type StepCompletedEvent = {
  type: "$step_completed";
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
};

export type PlanClearedEvent = { type: "$plan_cleared" };

export type SessionsEvent = {
  type: "$sessions";
  items: {
    name: string;
    messageCount: number;
    mtime: string;
    summary?: string;
    workspaceStatus?: "matched" | "legacy_missing_meta";
  }[];
};

export type ExternalSessionSource = "claude" | "codex";

export type ExternalSessionApp = {
  source: ExternalSessionSource;
  label: string;
  root: string;
  available: boolean;
  sessionCount: number;
  latestMtime?: string;
};

export type SessionImportSourcesEvent = {
  type: "$session_import_sources";
  apps: ExternalSessionApp[];
};

export type SessionImportResultEvent = {
  type: "$session_import_result";
  imported: number;
  skipped: number;
  failed: number;
};

export type MentionResultsEvent = {
  type: "$mention_results";
  nonce: number;
  query: string;
  results: string[];
};

export type MentionPreviewEvent = {
  type: "$mention_preview";
  nonce: number;
  path: string;
  head: string;
  totalLines: number;
};

export type PromptHistoryCursor = {
  sessionName: string;
  messageIndex: number;
};

export type PromptHistoryResultEvent = {
  type: "$prompt_history_result";
  nonce: number;
  entry: {
    value: string;
    cursor: PromptHistoryCursor;
  } | null;
};

export type TabOpenedEvent = {
  type: "$tab_opened";
  workspaceDir: string;
  /** True when the frontend should focus this tab (user-opened, or the restored focused tab). */
  active?: boolean;
};

export type TabClosedEvent = {
  type: "$tab_closed";
};

export type McpSpecStatus = "configured" | "handshake" | "connected" | "failed" | "disabled";

export type McpSpecInfo = {
  raw: string;
  name: string | null;
  transport: "stdio" | "sse" | "streamable-http";
  summary: string;
  config?: ImportedMcpServer;
  parseError?: string;
  status: McpSpecStatus;
  statusHint?: "auth" | "missing-token" | "command" | "network" | "unknown";
  statusReason?: string;
  toolCount?: number;
  tools?: McpToolInfo[];
};

export type McpToolInfo = {
  name: string;
  registeredName: string;
  description?: string;
};

export type McpSpecsEvent = {
  type: "$mcp_specs";
  specs: McpSpecInfo[];
  bridged: boolean;
};

export type ImportedMcpServer = {
  name: string;
  transport: "stdio" | "sse" | "streamable-http";
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  cwd?: string;
  url?: string;
  headers?: Record<string, string>;
  disabled?: boolean;
  requestTimeoutMs?: number;
};

export type SkillScope = "project" | "global" | "builtin";

export type SkillInfo = {
  name: string;
  description: string;
  scope: SkillScope;
  path: string;
  runAs: "inline" | "subagent";
  model?: string;
};

export type SkillsEvent = {
  type: "$skills";
  items: SkillInfo[];
};

export type CtxBreakdownEvent = {
  type: "$ctx_breakdown";
  reservedTokens: number;
  /** Current log token count (real-time) — sent after /compact to refresh the meter. */
  logTokens?: number;
};

export type MemoryEntryInfo = {
  kind: "project_file" | "global_file" | "structured";
  name: string;
  scope: "project" | "global";
  path: string;
  description: string;
  type?: string;
};

export type MemoryEvent = {
  type: "$memory";
  entries: MemoryEntryInfo[];
};

export type MemoryDetail = MemoryEntryInfo & {
  body: string;
  createdAt?: string;
};

export type MemoryDetailEvent = {
  type: "$memory_detail";
  detail: MemoryDetail;
};

export type RetryResultEvent = { type: "$retry_result"; text: string };

export type BtwResultEvent = { type: "$btw_result"; question: string; answer: string };

export type JobInfo = {
  id: number;
  tabId: string;
  sessionLabel: string;
  command: string;
  pid: number | null;
  running: boolean;
  exitCode: number | null;
  startedAt: number;
  outputTail: string;
  spawnError?: string;
};

export type JobsEvent = {
  type: "$jobs";
  items: JobInfo[];
};

export type LoadedSegment =
  | { kind: "text"; text: string }
  | { kind: "reasoning"; text: string }
  | {
      kind: "tool";
      callId: string;
      name: string;
      args: string;
      result?: string;
      ok?: boolean;
    };

export type LoadedMessage =
  | { kind: "user"; text: string }
  | {
      kind: "assistant";
      turn: number;
      segments: LoadedSegment[];
      pending: false;
    };

export type SessionLoadedEvent = {
  type: "$session_loaded";
  name: string;
  messages: LoadedMessage[];
  carryover: {
    totalCostUsd: number;
    cacheHitTokens: number;
    cacheMissTokens: number;
    totalCompletionTokens: number;
  };
};

export type SessionEmptyEvent = {
  type: "$session_empty";
  name: string;
  sizeBytes: number;
};

export type NeedsSetupEvent = {
  type: "$needs_setup";
  reason: "no_api_key";
};

export type EditMode = "review" | "auto" | "yolo" | "plan";

export type ReasoningEffort = "low" | "medium" | "high" | "max";

export type WebSearchEngineName =
  | "bing"
  | "bing-intl"
  | "searxng"
  | "metaso"
  | "baidu"
  | "tavily"
  | "perplexity"
  | "exa"
  | "brave"
  | "ollama";

export type SettingsEvent = {
  type: "$settings";
  reasoningEffort: ReasoningEffort;
  editMode: EditMode;
  budgetUsd: number | null;
  baseUrl?: string;
  apiKeyPrefix?: string;
  workspaceDir: string;
  recentWorkspaces: string[];
  model: string;
  editor?: string;
  desktopCloseBehavior?: "closeToTray" | "closeToQuit";
  webSearchEngine?: WebSearchEngineName;
  webSearchEndpoint?: string;
  webSearchApiKeys?: {
    metaso?: string;
    baidu?: string;
    tavily?: string;
    perplexity?: string;
    exa?: string;
    ollama?: string;
    brave?: string;
  };
  subagentModels?: Record<string, "flash" | "pro">;
  showSystemEvents?: boolean;
  version: string;
};

export type QQSettingsEvent = {
  type: "$qq_settings";
  appId?: string;
  appSecret?: string;
  sandbox: boolean;
  enabled: boolean;
  configured: boolean;
  runtimeState: "disconnected" | "connecting" | "connected" | "failed";
  lastError?: string;
  appIdPreview?: string;
  access: string;
};

export type BalanceInfoItem = {
  currency: string;
  total: number;
  granted?: number;
  toppedUp?: number;
};

export type BalanceEvent = {
  type: "$balance";
  currency: string;
  total: number;
  isAvailable: boolean;
  balanceInfos: BalanceInfoItem[];
};

export type SettingsPatch = {
  reasoningEffort?: ReasoningEffort;
  editMode?: EditMode;
  budgetUsd?: number | null;
  baseUrl?: string;
  workspaceDir?: string;
  recentWorkspaces?: string[];
  model?: string;
  editor?: string;
  desktopCloseBehavior?: "closeToTray" | "closeToQuit";
  webSearchEngine?: WebSearchEngineName;
  webSearchEndpoint?: string | null;
  metasoApiKey?: string | null;
  baiduApiKey?: string | null;
  tavilyApiKey?: string | null;
  perplexityApiKey?: string | null;
  exaApiKey?: string | null;
  ollamaApiKey?: string | null;
  braveApiKey?: string | null;
  subagentModels?: Record<string, "flash" | "pro">;
  /** Per-model context-window override (tokens). Keys are model ids; values are the prompt-side token cap. */
  contextTokens?: Record<string, number>;
  showSystemEvents?: boolean;
};

export type QQConfigPatch = {
  appId?: string;
  appSecret?: string;
  sandbox: boolean;
};

export type UserMessageEvent = {
  type: "user.message";
  id: number;
  ts: string;
  turn: number;
  text: string;
};

export type ModelTurnStartedEvent = {
  type: "model.turn.started";
  id: number;
  ts: string;
  turn: number;
  model: string;
  reasoningEffort: ReasoningEffort;
  prefixHash: string;
};

export type ModelDeltaEvent = {
  type: "model.delta";
  id: number;
  ts: string;
  turn: number;
  channel: "content" | "reasoning" | "tool_args";
  text: string;
};

export type Usage = {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  prompt_cache_hit_tokens?: number;
  prompt_cache_miss_tokens?: number;
};

export type ModelFinalEvent = {
  type: "model.final";
  id: number;
  ts: string;
  turn: number;
  content: string;
  reasoningContent?: string;
  usage?: Usage;
  costUsd?: number;
};

export type ToolPreparingEvent = {
  type: "tool.preparing";
  id: number;
  ts: string;
  turn: number;
  callId: string;
  name: string;
};

export type ToolIntentEvent = {
  type: "tool.intent";
  id: number;
  ts: string;
  turn: number;
  callId: string;
  name: string;
  args: string;
};

export type ToolResultEvent = {
  type: "tool.result";
  id: number;
  ts: string;
  turn: number;
  callId: string;
  ok: boolean;
  output: string;
};

export type StatusEvent = {
  type: "status";
  id: number;
  ts: string;
  turn: number;
  text: string;
};

export type WarningEvent = {
  type: "warning";
  id: number;
  ts: string;
  turn: number;
  text: string;
  severity: "low" | "high";
};

export type KernelErrorEvent = {
  type: "error";
  id: number;
  ts: string;
  turn: number;
  message: string;
  recoverable: boolean;
};

export type IncomingEvent = { tabId?: string } & (
  | ReadyEvent
  | ProtocolErrorEvent
  | TurnCompleteEvent
  | ConfirmRequiredEvent
  | PathAccessRequiredEvent
  | ChoiceRequiredEvent
  | PlanRequiredEvent
  | SessionsEvent
  | SessionImportSourcesEvent
  | SessionImportResultEvent
  | SessionLoadedEvent
  | SessionEmptyEvent
  | NeedsSetupEvent
  | SettingsEvent
  | QQSettingsEvent
  | BalanceEvent
  | CheckpointRequiredEvent
  | RevisionRequiredEvent
  | StepCompletedEvent
  | PlanClearedEvent
  | MentionResultsEvent
  | MentionPreviewEvent
  | PromptHistoryResultEvent
  | TabOpenedEvent
  | TabClosedEvent
  | McpSpecsEvent
  | SkillsEvent
  | CtxBreakdownEvent
  | MemoryEvent
  | MemoryDetailEvent
  | JobsEvent
  | UserMessageEvent
  | ModelTurnStartedEvent
  | ModelDeltaEvent
  | ModelFinalEvent
  | ToolPreparingEvent
  | ToolIntentEvent
  | ToolResultEvent
  | StatusEvent
  | WarningEvent
  | KernelErrorEvent
  | RetryResultEvent
  | BtwResultEvent
);

export type OutgoingCommand = { tabId?: string } & (
  | { cmd: "user_input"; text: string }
  | { cmd: "abort" }
  | { cmd: "confirm_response"; id: number; response: ConfirmationChoice }
  | { cmd: "choice_response"; id: number; response: ChoiceVerdict }
  | { cmd: "plan_response"; id: number; response: PlanVerdict }
  | { cmd: "checkpoint_response"; id: number; response: CheckpointVerdict }
  | { cmd: "revision_response"; id: number; response: RevisionVerdict }
  | { cmd: "session_list" }
  | { cmd: "session_delete"; name: string }
  | { cmd: "session_load"; name: string }
  | { cmd: "session_rename"; name: string; title: string }
  | { cmd: "session_import"; source: ExternalSessionSource; path: string; name?: string }
  | { cmd: "session_import_scan" }
  | { cmd: "session_import_bulk"; sources: ExternalSessionSource[] }
  | { cmd: "memory_read"; path: string }
  | { cmd: "new_chat" }
  | { cmd: "setup_save_key"; key: string }
  | { cmd: "settings_get" }
  | ({ cmd: "settings_save" } & SettingsPatch)
  | { cmd: "qq_status_get" }
  | { cmd: "qq_connect" }
  | { cmd: "qq_disconnect" }
  | ({ cmd: "qq_config_save" } & QQConfigPatch)
  | { cmd: "mention_query"; query: string; nonce: number }
  | { cmd: "mention_preview"; path: string; nonce: number }
  | { cmd: "mention_picked"; path: string }
  | {
      cmd: "prompt_history_step";
      nonce: number;
      direction: "older" | "newer";
      cursor?: PromptHistoryCursor | null;
      startSessionName?: string;
      stopSessionName?: string;
    }
  | { cmd: "tab_open"; workspaceDir?: string }
  | { cmd: "tab_close" }
  | { cmd: "tab_activate"; tabId: string }
  | { cmd: "mcp_specs_get" }
  | { cmd: "mcp_specs_add"; spec: string }
  | { cmd: "mcp_specs_remove"; spec: string }
  | { cmd: "mcp_import_servers"; servers: ImportedMcpServer[] }
  | { cmd: "mcp_specs_update"; raw: string; server: ImportedMcpServer }
  | { cmd: "mcp_specs_retry"; raw: string }
  | { cmd: "skills_get" }
  | { cmd: "skill_run"; name: string; args?: string }
  | { cmd: "jobs_list" }
  | { cmd: "jobs_stop"; jobId: number }
  | { cmd: "jobs_stop_all" }
  | { cmd: "compact_history" }
  | { cmd: "retry" }
  | { cmd: "btw"; text: string }
);
