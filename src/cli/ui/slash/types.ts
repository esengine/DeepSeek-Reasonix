import type { EditMode } from "../../../config.js";
import type { McpServerSummary } from "../../../mcp/summary.js";
import type { JobRegistry } from "../../../tools/jobs.js";
import type { PlanStep } from "../../../tools/plan.js";

export type { McpServerSummary } from "../../../mcp/summary.js";

export interface SlashResult {
  /** Text to display back to the user as a system/info line. */
  info?: string;
  /** Open the SessionPicker modal mid-chat — used by `/sessions` slash. */
  openSessionsPicker?: boolean;
  /** Open the CheckpointPicker modal — bare `/restore` (no name argument). */
  openCheckpointPicker?: boolean;
  /** Open the ModelPicker modal — bare `/model` (no id) opens it. */
  openModelPicker?: boolean;
  /** Open the unified MCP hub — `/mcp` defaults to "live", `/mcp browse` to "marketplace". */
  openMcpHub?: { tab: "live" | "marketplace" };
  /** Open the arg-completer picker for this command (e.g. `/language` → language picker). */
  openArgPickerFor?: string;
  /** Exit the app. */
  exit?: boolean;
  /** Clear the visible history. */
  clear?: boolean;
  /** Unknown command — display usage hint. */
  unknown?: boolean;
  /** `/retry` re-submit text — pushed back through the normal submit flow after log truncation. */
  resubmit?: string;
  /** Structured `/context` payload — `info` text can't carry per-segment color for the stacked bar. */
  ctxBreakdown?: {
    systemTokens: number;
    toolsTokens: number;
    logTokens: number;
    inputTokens: number;
    ctxMax: number;
    toolsCount: number;
    logMessages: number;
    topTools: Array<{ name: string; tokens: number; turn: number }>;
  };
  /** `/replay [N]` archived-plan payload — display-only, NEVER executed. */
  replayPlan?: {
    summary?: string;
    body?: string;
    steps: PlanStep[];
    completedStepIds: string[];
    completedAt: string;
    relativeTime: string;
    archiveBasename: string;
    /** 1-based index in `/plans` listing — surfaced in the header. */
    index: number;
    /** Total archives at the time of the lookup; helps the user navigate. */
    total: number;
  };
}

export interface SlashContext {
  mcpSpecs?: string[];
  codeUndo?: (args: readonly string[]) => string;
  codeApply?: (indices?: readonly number[]) => string;
  codeDiscard?: (indices?: readonly number[]) => string;
  codeHistory?: () => string;
  codeShowEdit?: (args: readonly string[]) => string;
  codeRoot?: string;
  pendingEditCount?: number;
  toolHistory?: () => Array<{ toolName: string; text: string }>;
  mcpServers?: McpServerSummary[];
  /** Absent → tests context; `/memory` MUST reply "root unknown" rather than silently reading wrong dir. */
  memoryRoot?: string;
  planMode?: boolean;
  editMode?: EditMode;
  setEditMode?: (mode: EditMode) => void;
  touchedFiles?: () => string[];
  /** stop_job is async; handlers return synchronously and let the registry resolve in the background. */
  jobs?: JobRegistry;
  postInfo?: (text: string) => void;
  /** Push a structured Doctor card with check-by-check status; used by `/doctor`. */
  postDoctor?: (
    checks: ReadonlyArray<{ label: string; level: "ok" | "warn" | "fail"; detail: string }>,
  ) => void;
  /** Push a verbose Usage card (full bars) — used by `/cost`; auto-emitted per-turn cards stay compact. */
  postUsage?: (args: {
    turn: number;
    promptTokens: number;
    reasonTokens: number;
    outputTokens: number;
    promptCap: number;
    cacheHit: number;
    cost: number;
    sessionCost: number;
    balance?: number;
    balanceCurrency?: string;
    elapsedMs?: number;
  }) => void;
  dispatch?: (event: import("../state/events.js").AgentEvent) => void;
  setPlanMode?: (on: boolean) => void;

  reloadHooks?: () => number;
  /** Diff config.mcp[] vs live bridges → add/close clients accordingly. Wired from chat.tsx mcpRuntime. */
  reloadMcp?: () => Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
    summaries: McpServerSummary[];
  }>;
  /** `null` → still in flight OR offline; consumers can't distinguish, so always offer `refreshLatestVersion`. */
  latestVersion?: string | null;
  refreshLatestVersion?: () => void;
  /** `null` → in flight / failed; `[]` → API answered empty. `/model <id>` warn-only since list can lag. */
  models?: string[] | null;
  refreshModels?: () => void;
  armPro?: () => void;
  disarmPro?: () => void;
  startLoop?: (intervalMs: number, prompt: string) => void;
  stopLoop?: () => void;
  getLoopStatus?: () => {
    prompt: string;
    intervalMs: number;
    iter: number;
    nextFireMs: number;
  } | null;
  startWalkthrough?: () => string;
  startDashboard?: () => Promise<string>;
  /** Tear the dashboard server down. Mirrors stopLoop's shape; no-op when not running. */
  stopDashboard?: () => Promise<void>;
  /** Snapshot the dashboard's URL when running, null otherwise. */
  getDashboardUrl?: () => string | null;
}

export type SlashGroup =
  | "chat"
  | "setup"
  | "info"
  | "session"
  | "extend"
  | "code"
  | "jobs"
  | "advanced";

export interface SlashCommandSpec {
  cmd: string;
  summary: string;
  contextual?: "code";
  /** Visual category in the suggestions palette + /help. `advanced` collapses by default. */
  group: SlashGroup;
  /** If the command takes args, hint text shown after the name. */
  argsHint?: string;
  /** First-arg picker source — file paths intentionally absent (use `@path` mentions instead). */
  argCompleter?: "models" | "mcp-resources" | "mcp-prompts" | readonly string[];
  /** Alternate names — typing any of these resolves to `cmd` for dispatch / suggestion / arg-context. */
  aliases?: readonly string[];
}

export interface SlashArgContext {
  spec: SlashCommandSpec;
  partial: string;
  partialOffset: number;
  kind: "picker" | "hint";
}
