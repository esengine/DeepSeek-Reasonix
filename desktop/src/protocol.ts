export type ReadyEvent = { type: "$ready" };
export type ProtocolErrorEvent = { type: "$error"; message: string };
export type TurnCompleteEvent = { type: "$turn_complete" };
export type ConfirmRequiredEvent = {
  type: "$confirm_required";
  id: number;
  kind: "run_command" | "run_background";
  command: string;
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

export type SessionsEvent = {
  type: "$sessions";
  items: { name: string; messageCount: number; mtime: string }[];
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

export type TabOpenedEvent = {
  type: "$tab_opened";
  workspaceDir: string;
};

export type TabClosedEvent = {
  type: "$tab_closed";
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
  };
};

export type NeedsSetupEvent = {
  type: "$needs_setup";
  reason: "no_api_key";
};

export type EditMode = "default" | "yolo" | "review";

export type PresetName = "auto" | "flash" | "pro";

export type SettingsEvent = {
  type: "$settings";
  reasoningEffort: "high" | "max";
  editMode: EditMode;
  budgetUsd: number | null;
  baseUrl?: string;
  apiKeyPrefix?: string;
  workspaceDir: string;
  recentWorkspaces: string[];
  model: string;
  preset: PresetName;
  editor?: string;
};

export type SettingsPatch = {
  reasoningEffort?: "high" | "max";
  editMode?: EditMode;
  budgetUsd?: number | null;
  baseUrl?: string;
  workspaceDir?: string;
  preset?: PresetName;
  editor?: string;
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
  reasoningEffort: "high" | "max";
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
  | ChoiceRequiredEvent
  | PlanRequiredEvent
  | SessionsEvent
  | SessionLoadedEvent
  | NeedsSetupEvent
  | SettingsEvent
  | MentionResultsEvent
  | MentionPreviewEvent
  | TabOpenedEvent
  | TabClosedEvent
  | UserMessageEvent
  | ModelTurnStartedEvent
  | ModelDeltaEvent
  | ModelFinalEvent
  | ToolPreparingEvent
  | ToolIntentEvent
  | ToolResultEvent
  | StatusEvent
  | KernelErrorEvent
);

export type OutgoingCommand = { tabId?: string } & (
  | { cmd: "user_input"; text: string }
  | { cmd: "abort" }
  | { cmd: "confirm_response"; id: number; response: ConfirmationChoice }
  | { cmd: "choice_response"; id: number; response: ChoiceVerdict }
  | { cmd: "plan_response"; id: number; response: PlanVerdict }
  | { cmd: "session_list" }
  | { cmd: "session_delete"; name: string }
  | { cmd: "session_load"; name: string }
  | { cmd: "new_chat" }
  | { cmd: "setup_save_key"; key: string }
  | { cmd: "settings_get" }
  | ({ cmd: "settings_save" } & SettingsPatch)
  | { cmd: "mention_query"; query: string; nonce: number }
  | { cmd: "mention_preview"; path: string; nonce: number }
  | { cmd: "mention_picked"; path: string }
  | { cmd: "tab_open"; workspaceDir?: string }
  | { cmd: "tab_close" }
);
