import { AsyncLocalStorage } from "node:async_hooks";
import { existsSync, statSync, writeSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { isAbsolute, join, resolve } from "node:path";
import { stdin } from "node:process";
import { createInterface } from "node:readline";
import { toApprovalPrompt } from "@reasonix/core-utils";
import {
  type FileWithStats,
  listDirectory,
  listFilesWithStatsAsync,
  parseAtQuery,
  rankPickerCandidates,
} from "../../at-mentions.js";
import { pickPrimaryBalance } from "../../client.js";
import { codeSystemPrompt } from "../../code/prompt.js";
import { applyPlanMode, buildCodeToolset } from "../../code/setup.js";
import {
  DEFAULT_MODEL,
  type DesktopCloseBehavior,
  type DesktopOpenTab,
  type EditMode,
  type McpServerConfig,
  type ReasonixConfig,
  bridgeEndpointEnv,
  isPlausibleKey,
  isReasoningEffort,
  loadApiKey,
  loadBaiduApiKey,
  loadBraveApiKey,
  loadDesktopCloseBehavior,
  loadDesktopOpenTabs,
  loadEditMode,
  loadEditor,
  loadEndpoint,
  loadExaApiKey,
  loadMaxIterPerTurn,
  loadMetasoApiKey,
  loadModel,
  loadOllamaApiKey,
  loadPerplexityApiKey,
  loadQQConfig,
  loadReasoningEffort,
  loadRecentWorkspaces,
  loadResolvedSkillPaths,
  loadShowSystemEvents,
  loadSubagentModels,
  loadTavilyApiKey,
  loadWorkspaceDir,
  normalizeMcpConfig,
  pushRecentWorkspace,
  readConfig,
  webSearchEngine as readWebSearchEngine,
  saveApiKey,
  saveBaseUrl,
  saveDesktopCloseBehavior,
  saveDesktopOpenTabs,
  saveEditMode,
  saveEditor,
  saveModel,
  saveReasoningEffort,
  saveShowSystemEvents,
  saveSubagentModels,
  saveWorkspaceDir,
  writeConfig,
} from "../../config.js";
import { Eventizer } from "../../core/eventize.js";
import type { Event as KernelEvent } from "../../core/events.js";
import {
  type CheckpointVerdict,
  type ChoiceVerdict,
  type ConfirmationChoice,
  type PlanVerdict,
  type RevisionVerdict,
  pauseGate,
} from "../../core/pause-gate.js";
import { autoResolveVerdict } from "../../core/pause-policy.js";
import { augmentProcessPath } from "../../desktop/login-shell-path.js";
import {
  type MemoryEntryDetail,
  type MemoryEntryInfo,
  collectMemoryEntriesForWorkspace,
  readMemoryEntryDetail,
} from "../../desktop/memory-browser.js";
import { classifyDesktopQQIngress } from "../../desktop/qq-ingress.js";
import {
  parseQQRemoteDesktopCommand,
  qqRemoteCommandBypassesBusy,
  qqRemoteDesktopHelpText,
} from "../../desktop/qq-remote-commands.js";
import {
  loadDesktopQQState,
  saveDesktopQQSettings,
  setDesktopQQEnabled,
} from "../../desktop/qq-settings.js";
import {
  clearQQTurnRouting,
  createQQTurnRoutingState,
  hasQQPendingInteraction,
  markQQTurnFinished,
  markQQTurnStarted,
  setQQPendingInteraction,
  shouldRouteQQForTab,
  takeQQPendingInteraction,
} from "../../desktop/qq-turn-routing.js";
import { loadDotenv } from "../../env.js";
import { type ResolvedHook, formatHookOutcomeMessage, loadHooks, runHooks } from "../../hooks.js";
import {
  CacheFirstLoop,
  DeepSeekClient,
  ImmutablePrefix,
  type LoopAbortOptions,
} from "../../index.js";
import { type McpServerSpec, parseMcpSpec, specToRaw } from "../../mcp/spec.js";
import {
  type PromptHistoryCursor,
  deleteSession,
  listSessionsForWorkspace,
  loadSessionMessages,
  loadSessionMeta,
  patchSessionMeta,
  patchSessionWorkspaceIfMissing,
  promptHistoryStep,
  sessionPath,
  timestampSuffix,
} from "../../memory/session.js";
import { QQChannel } from "../../qq/channel.js";
import {
  type ExternalSessionSource,
  discoverExternalSessionApps,
  importExternalSession,
  importExternalSessions,
} from "../../session-import.js";
import { SkillStore } from "../../skills.js";
import { countTokensBounded } from "../../tokenizer.js";
import type { ChoiceOption } from "../../tools/choice.js";
import type { ChatMessage } from "../../types.js";
import { VERSION } from "../../version.js";
import { type McpRuntime, createMcpRuntime } from "./mcp-runtime.js";

export interface DesktopOptions {
  model: string;
  budgetUsd?: number;
  /** Root directory the agent's filesystem tools operate inside. Defaults to cwd. */
  dir?: string;
}

export function desktopUserAbortLoopOptions(): LoopAbortOptions | undefined {
  // User-facing Abort stops generation; it must not erase a prompt that remains visible in chat.
  return undefined;
}

type InMessage = { tabId?: string } & (
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
  | {
      cmd: "settings_save";
      reasoningEffort?: import("../../config.js").ReasoningEffort;
      editMode?: EditMode;
      budgetUsd?: number | null;
      baseUrl?: string;
      workspaceDir?: string;
      recentWorkspaces?: string[];
      model?: string;
      editor?: string;
      desktopCloseBehavior?: DesktopCloseBehavior;
      webSearchEngine?:
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
      webSearchEndpoint?: string | null;
      metasoApiKey?: string | null;
      baiduApiKey?: string | null;
      tavilyApiKey?: string | null;
      perplexityApiKey?: string | null;
      exaApiKey?: string | null;
      ollamaApiKey?: string | null;
      braveApiKey?: string | null;
      subagentModels?: Record<string, "flash" | "pro">;
      contextTokens?: Record<string, number>;
      showSystemEvents?: boolean;
    }
  | { cmd: "qq_status_get" }
  | { cmd: "qq_connect" }
  | { cmd: "qq_disconnect" }
  | {
      cmd: "qq_config_save";
      appId?: string;
      appSecret?: string;
      sandbox: boolean;
    }
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
  | { cmd: "mcp_import_servers"; servers: unknown[] }
  | { cmd: "mcp_specs_update"; raw: string; server: unknown }
  | { cmd: "mcp_specs_retry"; raw: string }
  | { cmd: "skills_get" }
  | { cmd: "skill_run"; name: string; args?: string }
  | { cmd: "jobs_list" }
  | { cmd: "jobs_stop"; jobId: number }
  | { cmd: "jobs_stop_all" }
  | { cmd: "compact_history" }
  | { cmd: "retry" }
  | { cmd: "btw"; text: string }
  | { cmd: "desktop_resync" }
);

interface NeedsSetupEvent {
  type: "$needs_setup";
  reason: "no_api_key";
}

interface SettingsEvent {
  type: "$settings";
  reasoningEffort: import("../../config.js").ReasoningEffort;
  editMode: EditMode;
  budgetUsd: number | null;
  baseUrl?: string;
  apiKeyPrefix?: string;
  workspaceDir: string;
  recentWorkspaces: string[];
  model: string;
  editor?: string;
  desktopCloseBehavior?: DesktopCloseBehavior;
  webSearchEngine?:
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
  contextTokens?: Record<string, number>;
  showSystemEvents?: boolean;
  version: string;
}

interface QQSettingsEvent {
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
}

interface BalanceInfoItem {
  currency: string;
  total: number;
  granted?: number;
  toppedUp?: number;
}

interface BalanceEvent {
  type: "$balance";
  currency: string;
  total: number;
  isAvailable: boolean;
  balanceInfos: BalanceInfoItem[];
}

interface PlanRequiredEvent {
  type: "$plan_required";
  id: number;
  plan: string;
  steps?: unknown[];
  summary?: string;
}

interface SessionsEvent {
  type: "$sessions";
  items: {
    name: string;
    messageCount: number;
    mtime: string;
    summary?: string;
    workspaceStatus?: "matched" | "legacy_missing_meta";
  }[];
}

interface SessionImportSourcesEvent {
  type: "$session_import_sources";
  apps: ReturnType<typeof discoverExternalSessionApps>;
}

interface SessionImportResultEvent {
  type: "$session_import_result";
  imported: number;
  skipped: number;
  failed: number;
}

interface MentionResultsEvent {
  type: "$mention_results";
  nonce: number;
  query: string;
  results: string[];
}

interface MentionPreviewEvent {
  type: "$mention_preview";
  nonce: number;
  path: string;
  head: string;
  totalLines: number;
}

interface PromptHistoryResultEvent {
  type: "$prompt_history_result";
  nonce: number;
  entry: {
    value: string;
    cursor: PromptHistoryCursor;
  } | null;
}

interface TabOpenedEvent {
  type: "$tab_opened";
  workspaceDir: string;
  /** True when the frontend should focus this tab (user-opened, or the restored focused tab). */
  active?: boolean;
}

interface TabClosedEvent {
  type: "$tab_closed";
}

type LoadedSegment =
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

type LoadedMessage =
  | { kind: "user"; text: string }
  | {
      kind: "assistant";
      turn: number;
      segments: LoadedSegment[];
      pending: false;
    };

interface SessionLoadedEvent {
  type: "$session_loaded";
  name: string;
  messages: LoadedMessage[];
  carryover: {
    totalCostUsd: number;
    cacheHitTokens: number;
    cacheMissTokens: number;
    totalCompletionTokens: number;
  };
}

interface SessionEmptyEvent {
  type: "$session_empty";
  name: string;
  sizeBytes: number;
}

interface ConfirmRequiredEvent {
  type: "$confirm_required";
  id: number;
  kind: "run_command" | "run_background";
  command: string;
  prompt?: import("@reasonix/core-utils").ApprovalPrompt;
}

interface PathAccessRequiredEvent {
  type: "$path_access_required";
  id: number;
  path: string;
  intent: "read" | "write";
  toolName: string;
  sandboxRoot: string;
  allowPrefix: string;
  prompt?: import("@reasonix/core-utils").ApprovalPrompt;
}

interface ChoiceRequiredEvent {
  type: "$choice_required";
  id: number;
  question: string;
  options: ChoiceOption[];
  allowCustom: boolean;
}

interface PlanStepLite {
  id: string;
  title: string;
  action: string;
  risk?: "low" | "med" | "high";
}

interface CheckpointRequiredEvent {
  type: "$checkpoint_required";
  id: number;
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
  completed: number;
  total: number;
}

interface RevisionRequiredEvent {
  type: "$revision_required";
  id: number;
  reason: string;
  remainingSteps: PlanStepLite[];
  summary?: string;
}

interface StepCompletedEvent {
  type: "$step_completed";
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
}

interface PlanClearedEvent {
  type: "$plan_cleared";
}

type McpSpecStatus = "configured" | "handshake" | "connected" | "failed" | "disabled";
type McpStatusHint = "auth" | "missing-token" | "command" | "network" | "unknown";

interface McpSpecInfo {
  raw: string;
  name: string | null;
  transport: "stdio" | "sse" | "streamable-http";
  summary: string;
  config?: ImportedMcpServerInfo;
  parseError?: string;
  status: McpSpecStatus;
  statusHint?: McpStatusHint;
  statusReason?: string;
  toolCount?: number;
  tools?: McpToolInfo[];
}

interface McpToolInfo {
  name: string;
  registeredName: string;
  description?: string;
}

interface ImportedMcpServerInfo {
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
}

interface McpSpecsEvent {
  type: "$mcp_specs";
  specs: McpSpecInfo[];
  bridged: boolean;
}

interface CtxBreakdownEvent {
  type: "$ctx_breakdown";
  reservedTokens: number;
  /** Current log token count (real-time) — sent after /compact to refresh the meter. */
  logTokens?: number;
}

interface MemoryEvent {
  type: "$memory";
  entries: MemoryEntryInfo[];
}

interface MemoryDetailEvent {
  type: "$memory_detail";
  detail: MemoryEntryDetail;
}

interface SkillInfo {
  name: string;
  description: string;
  scope: "project" | "custom" | "global" | "builtin";
  path: string;
  runAs: "inline" | "subagent";
  model?: string;
}

interface SkillsEvent {
  type: "$skills";
  items: SkillInfo[];
}

interface JobInfoPayload {
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
}

interface JobsEvent {
  type: "$jobs";
  items: JobInfoPayload[];
}

const desktopQqRuntimeSnapshot: {
  runtimeState: "disconnected" | "connecting" | "connected" | "failed";
  lastError?: string;
} = {
  runtimeState: "disconnected",
};

interface RetryResultEvent {
  type: "$retry_result";
  text: string;
}

interface BtwResultEvent {
  type: "$btw_result";
  question: string;
  answer: string;
}

/** Direct fd write — bypasses Node's stream layer (and its piped-output
 *  block buffering) so every JSON line reaches Rust the moment it's
 *  produced, not whenever the next 8 KB flushes. */
type EmittableEvent =
  | KernelEvent
  | { type: "$ready" }
  | { type: "$error"; message: string }
  | { type: "$turn_complete" }
  | ConfirmRequiredEvent
  | PathAccessRequiredEvent
  | ChoiceRequiredEvent
  | PlanRequiredEvent
  | CheckpointRequiredEvent
  | RevisionRequiredEvent
  | StepCompletedEvent
  | PlanClearedEvent
  | SessionsEvent
  | SessionImportSourcesEvent
  | SessionImportResultEvent
  | SessionLoadedEvent
  | SessionEmptyEvent
  | NeedsSetupEvent
  | SettingsEvent
  | QQSettingsEvent
  | BalanceEvent
  | MentionResultsEvent
  | MentionPreviewEvent
  | PromptHistoryResultEvent
  | RetryResultEvent
  | BtwResultEvent
  | TabOpenedEvent
  | TabClosedEvent
  | McpSpecsEvent
  | SkillsEvent
  | CtxBreakdownEvent
  | MemoryEvent
  | MemoryDetailEvent
  | JobsEvent;

const STDOUT_BACKPRESSURE_WAIT = new Int32Array(new SharedArrayBuffer(4));

type SyncWriter = (fd: number, buffer: Buffer, offset: number, length: number) => number;

const SESSION_TITLE_MAX_CHARS = 200;

/** Trim + cap a user-provided session title; empty string means "clear summary". Exported for tests. */
export function normalizeSessionTitle(raw: string): string {
  return raw.replace(/\s+/g, " ").trim().slice(0, SESSION_TITLE_MAX_CHARS);
}

/** Return all MCP specs as raw strings, reading both legacy `cfg.mcp` and canonical `cfg.mcpServers`. */
export function getAllMcpSpecs(cfg: ReturnType<typeof readConfig>): string[] {
  return normalizeMcpConfig(cfg).map(specToRaw);
}

function normalizeStringRecord(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const out: Record<string, string> = {};
  for (const [key, entry] of Object.entries(value)) {
    if (typeof entry === "string" && entry.length > 0) out[key] = entry;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function normalizeStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.filter((entry): entry is string => typeof entry === "string");
}

export function normalizeImportedMcpServer(
  value: unknown,
): { name: string; config: McpServerConfig } | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const raw = value as Record<string, unknown>;
  const name = typeof raw.name === "string" ? raw.name.trim() : "";
  if (!name) return null;
  const cwd = typeof raw.cwd === "string" && raw.cwd.trim().length > 0 ? raw.cwd.trim() : undefined;
  const transport = raw.transport;
  if (transport === "stdio") {
    const command = typeof raw.command === "string" ? raw.command.trim() : "";
    if (!command) return null;
    return {
      name,
      config: {
        transport,
        command,
        args: normalizeStringArray(raw.args) ?? [],
        env: normalizeStringRecord(raw.env),
        cwd,
        disabled: raw.disabled === true ? true : undefined,
        requestTimeoutMs:
          typeof raw.requestTimeoutMs === "number" && Number.isFinite(raw.requestTimeoutMs)
            ? raw.requestTimeoutMs
            : undefined,
      },
    };
  }
  if (transport === "sse" || transport === "streamable-http") {
    const url = typeof raw.url === "string" ? raw.url.trim() : "";
    if (!url) return null;
    return {
      name,
      config: {
        transport,
        url,
        headers: normalizeStringRecord(raw.headers),
        disabled: raw.disabled === true ? true : undefined,
        requestTimeoutMs:
          typeof raw.requestTimeoutMs === "number" && Number.isFinite(raw.requestTimeoutMs)
            ? raw.requestTimeoutMs
            : undefined,
      },
    };
  }
  return null;
}

function rawForImportedMcpServer(name: string | null, config: McpServerConfig): string | undefined {
  if (config.transport === "stdio") {
    if (!config.command) return undefined;
    return specToRaw({
      name,
      transport: "stdio",
      command: config.command,
      args: config.args ?? [],
    });
  }
  if (config.transport === "sse" || config.transport === "streamable-http") {
    if (!config.url) return undefined;
    return specToRaw({
      name,
      transport: config.transport,
      url: config.url,
    });
  }
  return undefined;
}

function importedMcpServerFromSpec(spec: McpServerSpec): ImportedMcpServerInfo | undefined {
  if (!spec.name) return undefined;
  const base = {
    name: spec.name,
    transport: spec.transport,
    disabled: spec.disabled === true ? true : undefined,
    requestTimeoutMs: spec.requestTimeoutMs,
  };
  if (spec.transport === "stdio") {
    return {
      ...base,
      transport: "stdio",
      command: spec.command,
      args: spec.args,
      env: spec.env,
      cwd: spec.cwd,
    };
  }
  return {
    ...base,
    transport: spec.transport,
    url: spec.url,
    headers: spec.headers,
  };
}

function stripLegacyMcpConfigForNames(cfg: ReasonixConfig, names: ReadonlySet<string>): void {
  if (names.size === 0) return;
  if (Array.isArray(cfg.mcp) && cfg.mcp.length > 0) {
    cfg.mcp = cfg.mcp.filter((raw) => {
      try {
        const parsed = parseMcpSpec(raw);
        return !(parsed.name && names.has(parsed.name));
      } catch {
        return true;
      }
    });
    if (cfg.mcp.length === 0) cfg.mcp = undefined;
  }
  if (Array.isArray(cfg.mcpDisabled) && cfg.mcpDisabled.length > 0) {
    cfg.mcpDisabled = cfg.mcpDisabled.filter((name) => !names.has(name));
    if (cfg.mcpDisabled.length === 0) cfg.mcpDisabled = undefined;
  }
  if (cfg.mcpEnv) {
    for (const name of names) delete cfg.mcpEnv[name];
    if (Object.keys(cfg.mcpEnv).length === 0) cfg.mcpEnv = undefined;
  }
}

function legacyMcpRawMatches(entry: string, target: string): boolean {
  if (entry === target) return true;
  try {
    return specToRaw(parseMcpSpec(entry)) === target;
  } catch {
    return false;
  }
}

/** Remove the legacy raw spec being edited before saving its canonical `mcpServers` entry. */
export function stripLegacyMcpConfigForRaw(cfg: ReasonixConfig, raw: string): void {
  if (!Array.isArray(cfg.mcp) || cfg.mcp.length === 0) return;
  cfg.mcp = cfg.mcp.filter((entry) => !legacyMcpRawMatches(entry, raw));
  if (cfg.mcp.length === 0) cfg.mcp = undefined;
}

function importedMcpRawVariants(name: string, config: McpServerConfig): string[] {
  const variants = [rawForImportedMcpServer(name, config), rawForImportedMcpServer(null, config)];
  return [...new Set(variants.filter((raw): raw is string => typeof raw === "string"))];
}

export function applyImportedMcpServersToConfig(
  cfg: ReasonixConfig,
  servers: unknown[],
): { forceSpecs: string[] } {
  const canonical = { ...(cfg.mcpServers ?? {}) };
  const importedNames = new Set<string>();
  const forceSpecs = new Set<string>();
  const legacyRawSpecs = new Set<string>();
  for (const server of servers) {
    const normalized = normalizeImportedMcpServer(server);
    if (!normalized) continue;
    canonical[normalized.name] = normalized.config;
    importedNames.add(normalized.name);
    const [raw, ...legacyVariants] = importedMcpRawVariants(normalized.name, normalized.config);
    if (raw) forceSpecs.add(raw);
    for (const variant of legacyVariants) legacyRawSpecs.add(variant);
  }
  if (importedNames.size === 0) {
    throw new Error("no valid servers received");
  }
  cfg.mcpServers = canonical;
  stripLegacyMcpConfigForNames(cfg, importedNames);
  for (const raw of legacyRawSpecs) stripLegacyMcpConfigForRaw(cfg, raw);
  return { forceSpecs: [...forceSpecs] };
}

export function applyMcpSpecUpdateToConfig(
  cfg: ReasonixConfig,
  raw: string,
  server: unknown,
): { updatedRaw?: string; forceSpecs: string[] } {
  const normalized = normalizeImportedMcpServer(server);
  if (!normalized) {
    throw new Error("invalid server config");
  }
  const canonical = { ...(cfg.mcpServers ?? {}) };
  let oldName: string | undefined;
  try {
    oldName = parseMcpSpec(raw).name ?? undefined;
  } catch {
    oldName = undefined;
  }
  if (oldName && oldName !== normalized.name) {
    delete canonical[oldName];
  }
  canonical[normalized.name] = normalized.config;
  cfg.mcpServers = canonical;
  stripLegacyMcpConfigForRaw(cfg, raw);
  stripLegacyMcpConfigForNames(cfg, new Set([normalized.name, ...(oldName ? [oldName] : [])]));
  for (const variant of importedMcpRawVariants(normalized.name, normalized.config).slice(1)) {
    stripLegacyMcpConfigForRaw(cfg, variant);
  }
  if (Object.keys(cfg.mcpServers).length === 0) cfg.mcpServers = undefined;
  const updatedRaw = rawForImportedMcpServer(normalized.name, normalized.config);
  return { updatedRaw, forceSpecs: [...new Set([raw, ...(updatedRaw ? [updatedRaw] : [])])] };
}

/** Drain `buffer` to `fd` across partial writes; retry EAGAIN after a 5 ms park. Exported for tests. */
export function writeAllSync(
  fd: number,
  buffer: Buffer,
  opts: {
    write?: SyncWriter;
    wait?: () => void;
  } = {},
): void {
  const write = opts.write ?? writeSync;
  const wait = opts.wait ?? (() => Atomics.wait(STDOUT_BACKPRESSURE_WAIT, 0, 0, 5));
  let offset = 0;
  while (offset < buffer.length) {
    let written: number;
    try {
      written = write(fd, buffer, offset, buffer.length - offset);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === "EAGAIN") {
        wait();
        continue;
      }
      throw err;
    }
    if (written <= 0) throw new Error("stdout write returned 0 bytes");
    offset += written;
  }
}

function emit(ev: EmittableEvent, tabId?: string): void {
  const payload = tabId ? { ...ev, tabId } : ev;
  writeAllSync(1, Buffer.from(`${JSON.stringify(payload)}\n`, "utf8"));
}

function tailLines(s: string, n: number): string {
  if (!s) return "";
  const lines = s.split(/\r?\n/);
  return lines.slice(-n).join("\n");
}

const LOADED_RECENT_MESSAGE_WINDOW = 120;
const LOADED_MIN_ELIDE_CHARS = 4096;
const LOADED_ELIDED_PREFIX = "[elided — older than the last ";

function elideLoadedField(value: string): string {
  if (value.length <= LOADED_MIN_ELIDE_CHARS) return value;
  if (value.startsWith(LOADED_ELIDED_PREFIX)) return value;
  return `${LOADED_ELIDED_PREFIX}${LOADED_RECENT_MESSAGE_WINDOW} messages; ${value.length.toLocaleString()} chars dropped to save memory. Full content is on disk in the session log.]`;
}

function elideLoadedMessages(messages: LoadedMessage[]): LoadedMessage[] {
  if (messages.length < LOADED_RECENT_MESSAGE_WINDOW) return messages;
  const cutoff = messages.length - LOADED_RECENT_MESSAGE_WINDOW;
  return messages.map((msg, i) => {
    if (i >= cutoff || msg.kind !== "assistant") return msg;
    return {
      ...msg,
      segments: msg.segments.map((segment) => {
        switch (segment.kind) {
          case "reasoning":
          case "text":
            return { ...segment, text: elideLoadedField(segment.text) };
          case "tool":
            return {
              ...segment,
              args: elideLoadedField(segment.args),
              ...(segment.result !== undefined ? { result: elideLoadedField(segment.result) } : {}),
            };
          default:
            return segment;
        }
      }),
    };
  });
}

export function buildLoadedMessages(records: ChatMessage[]): LoadedMessage[] {
  const out: LoadedMessage[] = [];
  let turn = 0;
  let pendingAssistantIdx = -1;
  for (const rec of records) {
    if (rec.role === "system") continue;
    if (rec.role === "user") {
      out.push({ kind: "user", text: rec.content ?? "" });
      pendingAssistantIdx = -1;
      continue;
    }
    if (rec.role === "assistant") {
      turn++;
      const segments: LoadedSegment[] = [];
      if (rec.reasoning_content) segments.push({ kind: "reasoning", text: rec.reasoning_content });
      if (rec.content) segments.push({ kind: "text", text: rec.content });
      if (rec.tool_calls) {
        for (let i = 0; i < rec.tool_calls.length; i++) {
          const tc = rec.tool_calls[i];
          if (!tc) continue;
          segments.push({
            kind: "tool",
            callId: tc.id ?? `tc-r-${turn}-${i}`,
            name: tc.function?.name ?? "",
            args: tc.function?.arguments ?? "",
          });
        }
      }
      out.push({ kind: "assistant", turn, segments, pending: false });
      pendingAssistantIdx = out.length - 1;
      continue;
    }
    if (rec.role === "tool") {
      if (pendingAssistantIdx < 0) continue;
      const host = out[pendingAssistantIdx];
      if (host?.kind !== "assistant") continue;
      const callId = rec.tool_call_id;
      if (!callId) continue;
      const seg = host.segments.find((s) => s.kind === "tool" && s.callId === callId);
      if (seg && seg.kind === "tool") {
        seg.result = rec.content ?? "";
        seg.ok = !/error|failed/i.test(seg.result.slice(0, 200));
      }
    }
  }
  return elideLoadedMessages(out);
}

function maskApiKey(key: string | undefined): string | undefined {
  if (!key) return undefined;
  if (key.length <= 7) return `${key.slice(0, 2)}…`;
  return `${key.slice(0, 6)}…${key.slice(-3)}`;
}

function collectWebSearchApiKeyPrefixes(): {
  metaso?: string;
  baidu?: string;
  tavily?: string;
  perplexity?: string;
  exa?: string;
  ollama?: string;
  brave?: string;
} {
  return {
    metaso: maskApiKey(loadMetasoApiKey()),
    baidu: maskApiKey(loadBaiduApiKey()),
    tavily: maskApiKey(loadTavilyApiKey()),
    perplexity: maskApiKey(loadPerplexityApiKey()),
    exa: maskApiKey(loadExaApiKey()),
    ollama: maskApiKey(loadOllamaApiKey()),
    brave: maskApiKey(loadBraveApiKey()),
  };
}

function emitSettings(tab: Tab): void {
  const ep = loadEndpoint();
  const editMode = loadEditMode();
  if (tab.toolset) applyPlanMode(tab.toolset.tools, editMode);
  const recent = loadRecentWorkspaces().filter((p) => p !== tab.rootDir);
  emit(
    {
      type: "$settings",
      reasoningEffort: loadReasoningEffort(),
      editMode,
      budgetUsd: tab.runtime?.loop.budgetUsd ?? null,
      baseUrl: ep.baseUrl,
      apiKeyPrefix: ep.apiKey ? `${ep.apiKey.slice(0, 6)}…${ep.apiKey.slice(-3)}` : undefined,
      workspaceDir: tab.rootDir,
      recentWorkspaces: recent,
      model: tab.currentModel,
      editor: loadEditor(),
      desktopCloseBehavior: loadDesktopCloseBehavior(),
      webSearchEngine: readWebSearchEngine(),
      webSearchEndpoint: readConfig().webSearchEndpoint,
      webSearchApiKeys: collectWebSearchApiKeyPrefixes(),
      subagentModels: loadSubagentModels(),
      contextTokens: readConfig().contextTokens,
      showSystemEvents: loadShowSystemEvents(),
      version: VERSION,
    },
    tab.id,
  );
}

function emitQQSettings(tab: Tab): void {
  const base = loadDesktopQQState();
  emit(
    {
      type: "$qq_settings",
      ...base,
      runtimeState: desktopQqRuntimeSnapshot.runtimeState,
      lastError: desktopQqRuntimeSnapshot.lastError,
    },
    tab.id,
  );
}

async function emitBalance(tab: Tab): Promise<void> {
  if (!tab.runtime) return;
  const bal = await tab.runtime.loop.client.getBalance().catch(() => null);
  if (!bal) return;
  const primary = pickPrimaryBalance(bal.balance_infos);
  if (!primary) return;
  const balanceInfos = bal.balance_infos.map((info) => ({
    currency: info.currency,
    total: Number(info.total_balance),
    granted: info.granted_balance ? Number(info.granted_balance) : undefined,
    toppedUp: info.topped_up_balance ? Number(info.topped_up_balance) : undefined,
  }));
  emit(
    {
      type: "$balance",
      currency: primary.currency,
      total: Number(primary.total_balance),
      isAvailable: bal.is_available,
      balanceInfos,
    },
    tab.id,
  );
}

function emitSessions(tab: Tab): void {
  try {
    const items = listSessionsForWorkspace(tab.rootDir).map((s) => ({
      name: s.name,
      messageCount: s.messageCount,
      mtime: s.mtime.toISOString(),
      summary: s.meta.summary,
      workspaceStatus: s.workspaceStatus,
    }));
    emit({ type: "$sessions", items }, tab.id);
  } catch (err) {
    emit({ type: "$error", message: `session_list failed: ${(err as Error).message}` }, tab.id);
  }
}

function loadSessionIntoTab(
  tab: Tab,
  name: string,
  actions: {
    abortTurn: (tab: Tab) => void;
    cancelPendingGates: (tab: Tab) => void;
    persistOpenTabs: () => void;
  },
): void {
  const records = loadSessionMessages(name);
  const backfilledWorkspace = patchSessionWorkspaceIfMissing(name, tab.rootDir);
  const meta = loadSessionMeta(name);
  // Only set switching flag when there's a live turn to abort —
  // otherwise the flag stays true and suppresses the first turn's events (#1217).
  if (tab.aborter) tab.switching = true;
  actions.abortTurn(tab);
  actions.cancelPendingGates(tab);
  tab.currentSession = name;
  actions.persistOpenTabs();
  if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
  const loadedMessages = buildLoadedMessages(records);
  if (loadedMessages.length === 0) {
    let sizeBytes = 0;
    try {
      sizeBytes = statSync(sessionPath(name)).size;
    } catch {
      /* file may not exist */
    }
    process.stderr.write(
      `session_load: "${name}" returned 0 messages (file size=${sizeBytes}B) — empty or unreadable jsonl\n`,
    );
    emit({ type: "$session_empty", name, sizeBytes }, tab.id);
  }
  emit(
    {
      type: "$session_loaded",
      name,
      messages: loadedMessages,
      carryover: {
        totalCostUsd: meta.totalCostUsd ?? 0,
        cacheHitTokens: meta.cacheHitTokens ?? 0,
        cacheMissTokens: meta.cacheMissTokens ?? 0,
        totalCompletionTokens: meta.totalCompletionTokens ?? 0,
      },
    },
    tab.id,
  );
  emitCtxBreakdown(tab);
  if (backfilledWorkspace) emitSessions(tab);
}

function summarizeMcpSpec(raw: string): McpSpecInfo {
  try {
    const parsed = parseMcpSpec(raw);
    if (parsed.transport === "stdio") {
      const argv = [parsed.command, ...parsed.args].join(" ");
      return {
        raw,
        name: parsed.name,
        transport: "stdio",
        summary: `stdio · ${argv}`,
        status: "configured",
      };
    }
    return {
      raw,
      name: parsed.name,
      transport: parsed.transport,
      summary: `${parsed.transport} · ${parsed.url}`,
      status: "configured",
    };
  } catch (err) {
    return {
      raw,
      name: null,
      transport: "stdio",
      summary: raw,
      parseError: (err as Error).message,
      status: "failed",
      statusReason: (err as Error).message,
    };
  }
}

export function classifyMcpStatusReason(reason: string | undefined): McpStatusHint | undefined {
  if (!reason) return undefined;
  const lower = reason.toLowerCase();
  if (lower.includes("no bearer token") || lower.includes("missing bearer")) {
    return "missing-token";
  }
  if (
    lower.includes("401") ||
    lower.includes("unauthorized") ||
    lower.includes("invalid_token") ||
    lower.includes("authentication required") ||
    lower.includes("forbidden") ||
    lower.includes("403")
  ) {
    return "auth";
  }
  if (
    lower.includes("enoent") ||
    lower.includes("command not found") ||
    lower.includes("not found in path") ||
    lower.includes("spawn")
  ) {
    return "command";
  }
  if (
    lower.includes("timeout") ||
    lower.includes("econn") ||
    lower.includes("network") ||
    lower.includes("dns") ||
    lower.includes("fetch failed")
  ) {
    return "network";
  }
  return "unknown";
}

function mcpToolsForSummary(
  summary: ReturnType<McpRuntime["summaries"]>[number] | undefined,
): McpToolInfo[] {
  if (!summary || !summary.report.tools.supported) return [];
  const prefix = summary.bridgeEnv.prefix ?? "";
  return summary.report.tools.items.map((tool) => ({
    name: tool.name,
    registeredName: `${prefix}${tool.name}`,
    description: tool.description,
  }));
}

function emitMcpSpecs(tab: Tab): void {
  const cfg = readConfig();
  const allSpecs = normalizeMcpConfig(cfg);
  const summaries = new Map(
    (tab.mcpRuntime?.summaries() ?? []).map((summary) => [summary.spec, summary]),
  );
  const specs = allSpecs.map((spec) => {
    const raw = specToRaw(spec);
    const base = summarizeMcpSpec(raw);
    const live = tab.mcpStatuses.get(raw);
    const summary = summaries.get(raw);
    const tools = mcpToolsForSummary(summary);
    const withConfig = {
      ...base,
      config: importedMcpServerFromSpec(spec),
      toolCount: tools.length > 0 ? tools.length : summary?.toolCount,
      tools,
    };
    if (!live) return withConfig;
    return {
      ...withConfig,
      status: live.kind,
      statusHint: live.kind === "failed" ? classifyMcpStatusReason(live.reason) : undefined,
      statusReason: live.reason,
      toolCount: tools.length > 0 ? tools.length : live.toolCount,
    };
  });
  const bridged = specs.length > 0 && specs.every((s) => s.status === "connected");
  emit({ type: "$mcp_specs", specs, bridged }, tab.id);
}

function emitMemory(tab: Tab): void {
  try {
    const entries = collectMemoryEntriesForWorkspace(tab.rootDir);
    emit({ type: "$memory", entries }, tab.id);
  } catch (err) {
    emit({ type: "$error", message: `memory_get failed: ${(err as Error).message}` }, tab.id);
  }
}

function countTokensForMeter(text: string): number {
  try {
    return countTokensBounded(text);
  } catch {
    return text.length === 0 ? 0 : Math.max(1, Math.ceil(text.length * 0.3));
  }
}

// reserved = system prompt + tool specs, constant for the tab's lifetime once
// the loop is built. logTokens is refreshed during turns so Desktop doesn't
// show a fake zero while the streaming call is still waiting on usage metadata.
function emitCtxBreakdown(tab: Tab): void {
  if (!tab.runtime) return;
  const sys = countTokensForMeter(tab.runtime.loop.prefix.system);
  const tools = countTokensForMeter(JSON.stringify(tab.runtime.loop.prefix.toolSpecs));
  let logTokens = 0;
  try {
    logTokens = tab.runtime.loop.getCurrentLogTokens();
  } catch {
    for (const msg of tab.runtime.loop.log.toFullHistory()) {
      logTokens += countTokensForMeter(typeof msg.content === "string" ? msg.content : "");
      if (msg.role === "assistant" && Array.isArray(msg.tool_calls) && msg.tool_calls.length > 0) {
        logTokens += countTokensForMeter(JSON.stringify(msg.tool_calls));
      }
    }
  }
  emit({ type: "$ctx_breakdown", reservedTokens: sys + tools, logTokens }, tab.id);
}

function emitSkills(tab: Tab): void {
  try {
    const store = new SkillStore({
      projectRoot: tab.rootDir,
      customSkillPaths: loadResolvedSkillPaths(tab.rootDir),
      subagentModels: loadSubagentModels(),
    });
    const items = store.list().map((s) => ({
      name: s.name,
      description: s.description,
      scope: s.scope,
      path: s.path,
      runAs: s.runAs,
      model: s.model,
    }));
    emit({ type: "$skills", items }, tab.id);
  } catch (err) {
    emit({ type: "$error", message: `skills_get failed: ${(err as Error).message}` }, tab.id);
  }
}

interface RuntimeState {
  loop: CacheFirstLoop;
  eventizer: Eventizer;
  ctx: {
    model: string;
    prefixHash: string;
    reasoningEffort: import("../../config.js").ReasoningEffort;
  };
}

type SymbolEntry = { name: string; path: string; line: number; kind: string };

interface Tab {
  readonly id: string;
  rootDir: string;
  currentSession: string;
  currentModel: string;
  budgetUsd: number | undefined;
  /** null while the tab is bootstrapping — see `initTabToolset`. UI gates input on `$ready`, which only fires once this is set. */
  toolset: Awaited<ReturnType<typeof buildCodeToolset>> | null;
  /** Empty while bootstrapping; populated together with `toolset`. */
  system: string;
  runtime: RuntimeState | null;
  aborter: AbortController | null;
  fileIndex: FileWithStats[] | null;
  fileIndexBuilding: Promise<FileWithStats[]> | null;
  fileIndexBuiltAt: number;
  symbolIndex: SymbolEntry[] | null;
  symbolBuilding: Promise<SymbolEntry[]> | null;
  recentMentions: string[];
  /** Pause-gate ids waiting on this tab — abort uses these to free stranded plan_checkpoint / plan_revision / shell-confirm callers. */
  pendingGateIds: Set<number>;
  /** Step ids already marked complete in the in-flight plan — also tells UI when a plan is "active". */
  completedStepIds: Set<string>;
  /** Total steps in the in-flight plan (0 = no active plan / steps not provided). */
  planTotalSteps: number;
  mcpRuntime: McpRuntime | null;
  mcpStatuses: Map<string, { kind: McpSpecStatus; reason?: string; toolCount?: number }>;
  /** True while a session switch is in progress — prevents stale events from the old turn. */
  switching: boolean;
  hooks: ResolvedHook[];
}

let tabCounter = 0;
function nextTabId(): string {
  tabCounter++;
  return `t${tabCounter}`;
}

function mintSessionFor(rootDir: string): string {
  const name = `desktop-${timestampSuffix()}-${tabCounter}`;
  try {
    patchSessionMeta(name, { workspace: rootDir });
  } catch {
    // session meta is for filtering only — failure shouldn't block chat
  }
  return name;
}

function buildRuntimeFor(tab: Tab): RuntimeState {
  if (!tab.toolset) throw new Error("buildRuntimeFor called before initTabToolset finished");
  const toolset = tab.toolset;
  applyPlanMode(toolset.tools, loadEditMode());
  const ep = loadEndpoint();
  const client = new DeepSeekClient({ apiKey: ep.apiKey, baseUrl: ep.baseUrl });
  const prefix = new ImmutablePrefix({ system: tab.system, toolSpecs: toolset.tools.specs() });
  const reasoningEffort = loadReasoningEffort();
  const loop = new CacheFirstLoop({
    client,
    prefix,
    tools: toolset.tools,
    model: tab.currentModel,
    budgetUsd: tab.budgetUsd,
    session: tab.currentSession,
    reasoningEffort,
    maxIterPerTurn: loadMaxIterPerTurn(),
    hooks: tab.hooks,
    hookCwd: tab.rootDir,
  });
  const eventizer = new Eventizer();
  const ctx = { model: tab.currentModel, prefixHash: prefix.fingerprint, reasoningEffort };
  return { loop, eventizer, ctx };
}

const TS_EXPORT_RE =
  /^export\s+(?:default\s+)?(?:async\s+)?(function|class|const|let|var|interface|type|enum)\s+\*?\s*(\w+)/;

/** TTL on the in-memory file index — without this, files deleted / renamed since the last @ popup still show up as candidates. 10s balances "fresh enough for typical edit-then-mention flows" against "don't re-scan 5000 files on every keystroke". */
const FILE_INDEX_TTL_MS = 10_000;

async function getFileIndexFor(tab: Tab): Promise<FileWithStats[]> {
  const fresh = tab.fileIndex && Date.now() - tab.fileIndexBuiltAt < FILE_INDEX_TTL_MS;
  if (fresh) return tab.fileIndex as FileWithStats[];
  if (tab.fileIndexBuilding) return tab.fileIndexBuilding;
  tab.fileIndexBuilding = listFilesWithStatsAsync(tab.rootDir, { maxResults: 5000 })
    .then((res) => {
      tab.fileIndex = res;
      tab.fileIndexBuiltAt = Date.now();
      tab.fileIndexBuilding = null;
      return res;
    })
    .catch((err) => {
      tab.fileIndexBuilding = null;
      throw err;
    });
  return tab.fileIndexBuilding;
}

async function getSymbolIndexFor(tab: Tab): Promise<SymbolEntry[]> {
  if (tab.symbolIndex) return tab.symbolIndex;
  if (tab.symbolBuilding) return tab.symbolBuilding;
  tab.symbolBuilding = (async () => {
    const files = await getFileIndexFor(tab);
    const sourceExts = /\.(?:ts|tsx|js|jsx|mts|cts)$/;
    const candidates = files.filter((f) => sourceExts.test(f.path)).slice(0, 1500);
    const out: SymbolEntry[] = [];
    const PARALLEL = 16;
    for (let i = 0; i < candidates.length; i += PARALLEL) {
      const batch = candidates.slice(i, i + PARALLEL);
      await Promise.all(
        batch.map(async (entry) => {
          const abs = isAbsolute(entry.path) ? entry.path : join(tab.rootDir, entry.path);
          try {
            const text = await readFile(abs, "utf8");
            const lines = text.split(/\r?\n/);
            for (let li = 0; li < lines.length; li++) {
              const line = lines[li]!;
              if (!line.startsWith("export ")) continue;
              const m = TS_EXPORT_RE.exec(line);
              if (m) out.push({ kind: m[1]!, name: m[2]!, path: entry.path, line: li + 1 });
            }
          } catch {
            // unreadable / binary — skip
          }
        }),
      );
    }
    tab.symbolIndex = out;
    tab.symbolBuilding = null;
    return out;
  })().catch((err) => {
    tab.symbolBuilding = null;
    throw err;
  });
  return tab.symbolBuilding;
}

function rankSymbols(syms: readonly SymbolEntry[], q: string, limit: number): string[] {
  const needle = q.toLowerCase();
  const scored: { entry: SymbolEntry; score: number }[] = [];
  for (const s of syms) {
    const lower = s.name.toLowerCase();
    let score: number;
    if (lower === needle) score = 0;
    else if (lower.startsWith(needle)) score = 100;
    else if (lower.includes(needle)) score = 500 + lower.indexOf(needle);
    else continue;
    scored.push({ entry: s, score });
  }
  scored.sort((a, b) => a.score - b.score || a.entry.name.localeCompare(b.entry.name));
  return scored.slice(0, limit).map((s) => `${s.entry.path}:${s.entry.line}`);
}

function pushMentionRecent(tab: Tab, path: string): void {
  const MAX = 20;
  const idx = tab.recentMentions.indexOf(path);
  if (idx >= 0) tab.recentMentions.splice(idx, 1);
  tab.recentMentions.unshift(path);
  if (tab.recentMentions.length > MAX) tab.recentMentions.length = MAX;
}

/** The desktop sidecar is a long-running daemon — Tauri spawns this Node process once per app launch and pipes JSON over stdin/stdout. Without these handlers, any orphaned promise rejection (e.g. from an aborted turn whose cleanup races a session-switch — #1074) crashes the process with exit code 1, which the Tauri host surfaces as "reasonix exited (code 1)" and a full reconnect cycle. Log loudly so we can find the underlying bug, but don't take the daemon down. */
export function installDesktopCrashGuards(
  stderr: { write: (s: string) => unknown } = process.stderr,
): void {
  process.on("unhandledRejection", (reason) => {
    const err = reason instanceof Error ? reason : new Error(String(reason));
    stderr.write(`[desktop] unhandledRejection: ${err.stack ?? err.message}\n`);
  });
  process.on("uncaughtException", (err) => {
    stderr.write(`[desktop] uncaughtException: ${err.stack ?? err.message}\n`);
  });
}

export async function desktopCommand(opts: DesktopOptions): Promise<void> {
  loadDotenv();
  // Tauri spawns the bundled Node from the GUI process, which never runs the
  // user's shell init (`.bashrc` / `.zshrc` / profile). Probe the login shell
  // once so nvm / asdf / fnm / volta / mise PATH entries reach `run_command`
  // children too (#1252). No-op on Windows — system PATH already covers GUI apps.
  const augmented = augmentProcessPath();
  if (augmented.added.length > 0) {
    process.stderr.write(
      `[desktop] augmented PATH with ${augmented.added.length} login-shell entries\n`,
    );
  }
  installDesktopCrashGuards();

  const tabs = new Map<string, Tab>();
  const tabContext = new AsyncLocalStorage<string>();
  // Frontend-reported focused tab — persisted so a restart reopens on it (#1244).
  let lastActiveTabId = "";

  function activeRunningTab(): Tab | undefined {
    const id = tabContext.getStore();
    return id ? tabs.get(id) : undefined;
  }

  let first: Tab;

  const qqRuntime = {
    channel: null as QQChannel | null,
    runtimeState: "disconnected" as "disconnected" | "connecting" | "connected" | "failed",
    lastError: undefined as string | undefined,
    routing: createQQTurnRoutingState(),
  };

  function currentQqSettings(): QQSettingsEvent {
    const base = loadDesktopQQState();
    return {
      type: "$qq_settings",
      ...base,
      runtimeState: qqRuntime.runtimeState,
      lastError: qqRuntime.lastError,
    };
  }

  function activeDesktopTab(): Tab | undefined {
    return (lastActiveTabId ? tabs.get(lastActiveTabId) : undefined) ?? first;
  }

  function broadcastQQSettings(): void {
    for (const tab of tabs.values()) emit(currentQqSettings(), tab.id);
  }

  function setQQRuntimeState(
    runtimeState: "disconnected" | "connecting" | "connected" | "failed",
    lastError?: string,
  ): void {
    qqRuntime.runtimeState = runtimeState;
    qqRuntime.lastError = lastError;
    desktopQqRuntimeSnapshot.runtimeState = runtimeState;
    desktopQqRuntimeSnapshot.lastError = lastError;
    broadcastQQSettings();
  }

  function sendQQInfo(message: string, tabOverride?: Tab): void {
    const tab = tabOverride ?? activeDesktopTab();
    if (tab) {
      emit(
        {
          type: "status",
          id: Date.now(),
          ts: new Date().toISOString(),
          turn: 0,
          text: message,
        },
        tab.id,
      );
    }
    void qqRuntime.channel?.sendResponse(message).catch((err) => {
      const active = activeDesktopTab();
      if (active) {
        emit({ type: "$error", message: `qq send failed: ${(err as Error).message}` }, active.id);
      }
    });
  }

  function emitQQNotice(message: string, tabOverride?: Tab): void {
    const tab = tabOverride ?? activeDesktopTab();
    if (tab) {
      emit(
        {
          type: "warning",
          id: Date.now(),
          ts: new Date().toISOString(),
          turn: 0,
          text: message,
          severity: "high",
        },
        tab.id,
      );
    }
  }

  function startNewChatInTab(tab: Tab): void {
    if (tab.aborter) tab.switching = true;
    abortTurn(tab);
    cancelPendingGates(tab);
    tab.currentSession = mintSessionFor(tab.rootDir);
    persistOpenTabs();
    if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
    emitSessions(tab);
  }

  function buildSkillPayload(tab: Tab, name: string, args?: string): string | null {
    const store = new SkillStore({
      projectRoot: tab.rootDir,
      customSkillPaths: loadResolvedSkillPaths(tab.rootDir),
    });
    const found = store.read(name);
    if (!found) return null;
    const extra = args?.trim() ?? "";
    const header = `# Skill: ${found.name}${found.description ? `\n> ${found.description}` : ""}`;
    const argsLine = extra ? `\n\nArguments: ${extra}` : "";
    return `${header}\n\n${found.body}${argsLine}`;
  }

  function availableSkillNamesForTab(tab: Tab): string[] {
    const store = new SkillStore({
      projectRoot: tab.rootDir,
      customSkillPaths: loadResolvedSkillPaths(tab.rootDir),
      subagentModels: loadSubagentModels(),
    });
    return store.list().map((s) => s.name);
  }

  function runBtwOnTab(
    tab: Tab,
    question: string,
    hooks?: {
      onAnswer?: (answer: string) => void;
      onError?: (message: string) => void;
    },
  ): void {
    if (!tab.runtime) return;
    void (async () => {
      try {
        const reply = await tab.runtime!.loop.client.chat({
          model: tab.currentModel,
          messages: [
            {
              role: "system",
              content:
                "You are answering a side question that is unrelated to the current coding conversation. Answer concisely (1-3 sentences) in plain prose. Do not call tools, do not ask clarifying questions, and do not reference any prior turns.",
            },
            { role: "user", content: question },
          ],
        });
        const answer =
          (typeof reply.content === "string" ? reply.content.trim() : "") || "(no answer)";
        emit({ type: "$btw_result", question, answer }, tab.id);
        hooks?.onAnswer?.(answer);
      } catch (err) {
        const message = `/btw failed: ${(err as Error).message}`;
        emit({ type: "$error", message }, tab.id);
        hooks?.onError?.(message);
      }
    })();
  }

  function handleQQRemoteDesktopCommand(tab: Tab, text: string): boolean {
    const cmd = parseQQRemoteDesktopCommand(text, availableSkillNamesForTab(tab));
    if (!cmd) return false;
    if (tab.aborter && !qqRemoteCommandBypassesBusy(cmd)) {
      void qqRuntime.channel
        ?.sendResponse("Session is busy. Wait for the current turn or reply to the pending prompt.")
        .catch(() => undefined);
      return true;
    }
    switch (cmd.kind) {
      case "help":
        sendQQInfo(qqRemoteDesktopHelpText(availableSkillNamesForTab(tab)), tab);
        return true;
      case "abort":
        abortTurn(tab, { discardCurrentTurn: true });
        cancelPendingGates(tab);
        sendQQInfo("Stopped the current desktop conversation.", tab);
        return true;
      case "new":
        startNewChatInTab(tab);
        sendQQInfo("Started a new desktop conversation in the current tab.", tab);
        return true;
      case "compact":
        if (!tab.runtime) {
          sendQQInfo("Desktop is not configured yet.", tab);
          return true;
        }
        void tab.runtime.loop
          .compactHistory()
          .then(() => {
            emitCtxBreakdown(tab);
            sendQQInfo("Compacted the current desktop conversation history.", tab);
          })
          .catch((err: Error) => {
            emit({ type: "$error", message: `/compact failed: ${err.message}` }, tab.id);
            void qqRuntime.channel
              ?.sendResponse(`/compact failed: ${err.message}`)
              .catch(() => undefined);
          });
        return true;
      case "retry": {
        if (!tab.runtime) {
          sendQQInfo("Desktop is not configured yet.", tab);
          return true;
        }
        const prev = tab.runtime.loop.retryLastUser();
        if (!prev) {
          sendQQInfo(
            "There is no previous local user message to retry in this desktop conversation.",
            tab,
          );
          return true;
        }
        void runTurn(tab, prev, true);
        return true;
      }
      case "model": {
        if (!cmd.value) {
          sendQQInfo(
            `Current model: ${tab.currentModel}. Use /model flash, /model pro, /model deepseek-v4-flash, or /model deepseek-v4-pro.`,
            tab,
          );
          return true;
        }
        const next = normalizeQQRemoteModel(cmd.value);
        if (!next) {
          sendQQInfo(
            "Unsupported desktop model. Use /model flash, /model pro, /model deepseek-v4-flash, or /model deepseek-v4-pro.",
            tab,
          );
          return true;
        }
        applyDesktopModel(tab, next);
        sendQQInfo(`Switched desktop model to ${next}.`, tab);
        return true;
      }
      case "effort":
        if (!cmd.value) {
          sendQQInfo(
            `Current reasoning effort: ${loadReasoningEffort()}. Use /effort low, /effort medium, /effort high, or /effort max.`,
            tab,
          );
          return true;
        }
        saveReasoningEffort(cmd.value);
        tab.runtime?.loop.configure({ reasoningEffort: cmd.value });
        emitSettings(tab);
        sendQQInfo(`Switched desktop reasoning effort to ${cmd.value}.`, tab);
        return true;
      case "plan":
        if (!cmd.value) {
          sendQQInfo(
            `Current plan mode: ${loadEditMode()}. Use /plan review, /plan auto, or /plan yolo.`,
            tab,
          );
          return true;
        }
        saveEditMode(cmd.value);
        if (tab.toolset) applyPlanMode(tab.toolset.tools, cmd.value);
        emitSettings(tab);
        sendQQInfo(`Switched desktop plan mode to ${cmd.value}.`, tab);
        return true;
      case "btw":
        if (!tab.runtime) {
          sendQQInfo("Desktop is not configured yet.", tab);
          return true;
        }
        runBtwOnTab(tab, cmd.text, {
          onAnswer: (answer) =>
            void qqRuntime.channel?.sendResponse(`≫ btw\n${answer}`).catch(() => undefined),
          onError: (message) =>
            void qqRuntime.channel?.sendResponse(message).catch(() => undefined),
        });
        return true;
      case "skill": {
        if (!tab.runtime) {
          sendQQInfo("Desktop is not configured yet.", tab);
          return true;
        }
        const payload = buildSkillPayload(tab, cmd.name, cmd.args);
        if (!payload) {
          emit({ type: "$error", message: `skill not found: ${cmd.name}` }, tab.id);
          void qqRuntime.channel
            ?.sendResponse(`skill not found: ${cmd.name}`)
            .catch(() => undefined);
          return true;
        }
        void runTurn(tab, payload, true);
        return true;
      }
      default:
        return false;
    }
  }

  function normalizeQQRemoteModel(value: string): string | null {
    const lower = value.trim().toLowerCase();
    if (!lower) return null;
    if (lower === "flash") return "deepseek-v4-flash";
    if (lower === "pro") return "deepseek-v4-pro";
    if (lower === "deepseek-v4-flash" || lower === "deepseek-v4-pro") return lower;
    return null;
  }

  function applyDesktopModel(tab: Tab, next: string): void {
    tab.currentModel = next;
    saveModel(next);
    if (tab.toolset) {
      tab.system = codeSystemPrompt(tab.rootDir, {
        hasSemanticSearch: tab.toolset.semantic.enabled,
        modelId: tab.currentModel,
      });
      if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
    }
    emitSettings(tab);
  }

  function parseIndexedChoice(text: string): number {
    const rawIndex = text.match(/^(\d+)/)?.[1];
    return rawIndex ? Number.parseInt(rawIndex, 10) - 1 : -1;
  }

  function parseRunPermissionChoice(text: string): "run_once" | "always_allow" | "deny" {
    const lower = text.toLowerCase();
    if (lower.includes("1") || lower.includes("run")) return "run_once";
    if (lower.includes("2") || lower.includes("always")) return "always_allow";
    return "deny";
  }

  function parsePlanChoice(text: string): "approve" | "refine" | "cancel" {
    const lower = text.toLowerCase();
    if (lower.includes("1") || lower.includes("approve")) return "approve";
    if (lower.includes("2") || lower.includes("refine")) return "refine";
    return "cancel";
  }

  function parseCheckpointChoice(text: string): "continue" | "revise" | "stop" {
    const lower = text.toLowerCase();
    if (lower.includes("1") || lower.includes("continue")) return "continue";
    if (lower.includes("2") || lower.includes("revise")) return "revise";
    return "stop";
  }

  function parseRevisionChoice(text: string): "accept" | "reject" | "cancel" {
    const lower = text.toLowerCase();
    if (lower.includes("1") || lower.includes("accept")) return "accept";
    if (lower.includes("2") || lower.includes("reject")) return "reject";
    return "cancel";
  }

  function stripFollowupPrefix(text: string): string {
    return text
      .replace(
        /^(?:\d+\s*|approve\s*|refine\s*|cancel\s*|continue\s*|revise\s*|stop\s*|accept\s*|reject\s*|run\s*|always\s*|deny\s*)/iu,
        "",
      )
      .trim();
  }

  function handleQQPauseReply(tab: Tab, text: string): boolean {
    const pending = takeQQPendingInteraction(qqRuntime.routing, tab.id);
    if (!pending) return false;
    const followup = stripFollowupPrefix(text);
    const interaction = pending;
    const gateId = pending.gateId;

    switch (interaction.kind) {
      case "run_command":
      case "run_background":
      case "path_access":
        pauseGate.resolve(gateId, parseRunPermissionChoice(text));
        return true;
      case "plan_proposed": {
        const payload = (interaction.payload as { plan?: string }) ?? {};
        const choice = parsePlanChoice(text);
        if (choice === "cancel") {
          pauseGate.cancel(gateId);
        } else {
          pauseGate.resolve(gateId, {
            type: choice === "approve" ? "approve" : "refine",
            feedback: followup,
            override: {
              plan: payload.plan ?? "",
              mode: choice === "approve" ? "approve" : "refine",
            },
          });
        }
        return true;
      }
      case "plan_checkpoint": {
        const payload = (interaction.payload as { stepId?: string; title?: string }) ?? {};
        const choice = parseCheckpointChoice(text);
        if (choice === "revise") {
          pauseGate.resolve(gateId, {
            type: "revise",
            feedback: followup,
            checkpoint: { stepId: payload.stepId ?? "", title: payload.title },
          });
        } else {
          pauseGate.resolve(gateId, { type: choice });
        }
        return true;
      }
      case "plan_revision":
        pauseGate.resolve(gateId, parseRevisionChoice(text));
        return true;
      case "choice": {
        const payload =
          (interaction.payload as { options?: ChoiceOption[]; allowCustom?: boolean }) ?? {};
        const options = payload.options ?? [];
        const pickedIndex = parseIndexedChoice(text);
        if (pickedIndex >= 0 && pickedIndex < options.length) {
          const selected = options[pickedIndex];
          if (selected) pauseGate.resolve(gateId, { type: "pick", optionId: selected.id });
          return true;
        }
        for (const option of options) {
          if (text.toLowerCase().includes(option.title.toLowerCase())) {
            pauseGate.resolve(gateId, { type: "pick", optionId: option.id });
            return true;
          }
        }
        pauseGate.resolve(
          gateId,
          payload.allowCustom ? { type: "text", text } : { type: "cancel" },
        );
        return true;
      }
      default:
        return false;
    }
  }

  function handleQQPauseRequest(tab: Tab, kind: string, payload: Record<string, unknown>): void {
    if (!qqRuntime.channel || !shouldRouteQQForTab(qqRuntime.routing, tab.id)) return;
    let qqMessage = "";
    switch (kind) {
      case "run_command":
      case "run_background": {
        const p = payload as { command: string };
        qqMessage = `Need confirmation\n\nCommand: \`${p.command}\`\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
        break;
      }
      case "path_access": {
        const p = payload as { path: string; intent: "read" | "write"; toolName: string };
        const intentText = p.intent === "read" ? "Read" : "Write";
        qqMessage = `Need file access confirmation\n\nAction: ${intentText}\nPath: ${p.path}\nTool: ${p.toolName}\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
        break;
      }
      case "plan_proposed": {
        const p = payload as { plan: string };
        qqMessage = `Plan confirmation\n\n${p.plan}\n\nReply with:\n1. Approve\n2. Refine\n3. Cancel`;
        break;
      }
      case "plan_checkpoint": {
        const p = payload as { title?: string; result: string };
        qqMessage = `Step complete (${tab.completedStepIds.size}/${tab.planTotalSteps})\n\n${
          p.title ? `Step: ${p.title}\n` : ""
        }Result: ${p.result}\n\nReply with:\n1. Continue\n2. Revise\n3. Stop`;
        break;
      }
      case "plan_revision": {
        const p = payload as { reason: string };
        qqMessage = `Plan revision proposed\n\n${p.reason}\n\nReply with:\n1. Accept\n2. Reject\n3. Cancel`;
        break;
      }
      case "choice": {
        const p = payload as { question: string; options: ChoiceOption[]; allowCustom: boolean };
        const optionsList = p.options.map((opt, idx) => `${idx + 1}. ${opt.title}`).join("\n");
        qqMessage = `Please choose\n\n${p.question}\n\nOptions:\n${optionsList}${
          p.allowCustom ? "\n\n(You can also reply with custom text.)" : ""
        }`;
        break;
      }
    }
    if (qqMessage) {
      void qqRuntime.channel.sendResponse(qqMessage).catch((err) => {
        emit({ type: "$error", message: `qq send failed: ${(err as Error).message}` }, tab.id);
      });
    }
  }

  async function startDesktopQQ(shouldPersistEnabled = true): Promise<void> {
    const current = loadQQConfig();
    if (!(current.appId && current.appSecret)) {
      throw new Error("QQ App ID and App Secret are required.");
    }
    if (qqRuntime.channel) {
      qqRuntime.channel.refreshAccessConfig();
      setQQRuntimeState("connected");
      return;
    }
    setQQRuntimeState("connecting");
    const channel = new QQChannel({
      onSubmitMessage: (text) => {
        const tab = activeDesktopTab();
        if (!tab) return;
        const trimmed = text.trim();
        if (!trimmed) return;
        if (handleQQRemoteDesktopCommand(tab, trimmed)) return;
        const decision = classifyDesktopQQIngress({
          hasPendingInteraction: hasQQPendingInteraction(qqRuntime.routing, tab.id),
          isBusy: !!tab.aborter,
        });
        if (decision === "pause_reply") {
          handleQQPauseReply(tab, trimmed);
          return;
        }
        if (decision === "busy") {
          void channel
            .sendResponse(
              "Session is busy. Wait for the current turn or reply to the pending prompt.",
            )
            .catch(() => undefined);
          return;
        }
        emit(
          {
            type: "user.message",
            id: Date.now(),
            ts: new Date().toISOString(),
            turn: 0,
            text: trimmed,
          },
          tab.id,
        );
        void runTurn(tab, trimmed, true);
      },
      onError: (message) => {
        const tab = activeDesktopTab();
        setQQRuntimeState("failed", message);
        if (tab) emit({ type: "$error", message: `QQ: ${message}` }, tab.id);
      },
      onInfo: (message) => emitQQNotice(`QQ: ${message}`),
    });
    try {
      await channel.start();
      qqRuntime.channel = channel;
      if (shouldPersistEnabled) setDesktopQQEnabled(true);
      setQQRuntimeState("connected");
    } catch (err) {
      await channel.stop().catch(() => undefined);
      qqRuntime.channel = null;
      if (shouldPersistEnabled) setDesktopQQEnabled(false);
      setQQRuntimeState("failed", (err as Error).message);
      throw err;
    }
  }

  async function stopDesktopQQ(shouldDisable = true): Promise<void> {
    const channel = qqRuntime.channel;
    qqRuntime.channel = null;
    clearQQTurnRouting(qqRuntime.routing);
    if (channel) {
      try {
        await channel.stop();
      } catch (err) {
        setQQRuntimeState("failed", (err as Error).message);
        throw err;
      }
    }
    if (shouldDisable) setDesktopQQEnabled(false);
    setQQRuntimeState("disconnected");
  }

  /** Synchronous tab construction — no I/O. All cheap, disk-only events (`$settings`, `$sessions`, `$memory`, `$skills`, `$mcp_specs`) can fire against this immediately. The heavy bits (`buildCodeToolset`, MCP probes, runtime construction) happen in `initTabToolset` so the UI shell paints without waiting for them. */
  function createTabSkeleton(initialDir?: string): Tab {
    const dir = resolve(initialDir ?? opts.dir ?? loadWorkspaceDir() ?? process.cwd());
    pushRecentWorkspace(dir);
    const model = opts.model || loadModel() || DEFAULT_MODEL;
    const tab: Tab = {
      id: nextTabId(),
      rootDir: dir,
      currentSession: "",
      currentModel: model,
      budgetUsd: opts.budgetUsd,
      toolset: null,
      system: "",
      runtime: null,
      aborter: null,
      fileIndex: null,
      fileIndexBuilding: null,
      fileIndexBuiltAt: 0,
      symbolIndex: null,
      symbolBuilding: null,
      recentMentions: [],
      pendingGateIds: new Set<number>(),
      completedStepIds: new Set<string>(),
      planTotalSteps: 0,
      mcpRuntime: null,
      mcpStatuses: new Map(),
      switching: false,
      hooks: loadHooks({ projectRoot: dir }),
    };
    tab.currentSession = mintSessionFor(dir);
    tabs.set(tab.id, tab);
    return tab;
  }

  /** Builds the toolset / system prompt / runtime / MCP bridge for a freshly-created skeleton. Reads `tab.currentModel` at call time so model changes during the wait are honored. */
  async function initTabToolset(tab: Tab): Promise<void> {
    const toolset = await buildCodeToolset({
      rootDir: tab.rootDir,
      onSkillInstalled: () => emitSkills(tab),
      onJobsChanged: () => emitJobs(),
    });
    tab.toolset = toolset;
    tab.system = codeSystemPrompt(tab.rootDir, {
      hasSemanticSearch: toolset.semantic.enabled,
      modelId: tab.currentModel,
    });
    if (loadApiKey()) {
      bridgeEndpointEnv();
      tab.runtime = buildRuntimeFor(tab);
      void bridgeTabMcp(tab);
    }
  }

  function bridgeTabMcp(tab: Tab, opts: { forceSpecs?: string[] } = {}): Promise<void> {
    if (!tab.runtime || !tab.toolset) return Promise.resolve();
    if (tab.mcpRuntime) {
      // Already constructed — reload so new/removed specs settle without restart.
      return tab.mcpRuntime
        .reloadFromConfig(tab.runtime.loop, { force: opts.forceSpecs })
        .then(() => emitMcpSpecs(tab))
        .catch((err) => {
          emit({ type: "$error", message: `mcp reload failed: ${(err as Error).message}` }, tab.id);
        });
    }
    const allSpecs = getAllMcpSpecs(readConfig());
    const requested = allSpecs.length;
    if (requested === 0) return Promise.resolve();
    const runtime = createMcpRuntime({
      getTools: () => {
        if (!tab.toolset) throw new Error("toolset gone");
        return tab.toolset.tools;
      },
      getMcpPrefix: () => undefined,
      getRequestedCount: () => requested,
      getWorkspaceDir: () => tab.rootDir,
      progressSink: { current: null },
    });
    tab.mcpRuntime = runtime;
    runtime.setLifecycleSink((notice) => {
      if (notice.kind === "slow") return; // not surfaced in the desktop panel
      const specs = getAllMcpSpecs(readConfig());
      const target = specs.find((raw) => {
        try {
          return parseMcpSpec(raw).name === notice.name;
        } catch {
          return false;
        }
      });
      if (!target) return;
      if (notice.kind === "handshake") {
        tab.mcpStatuses.set(target, { kind: "handshake" });
      } else if (notice.kind === "connected") {
        tab.mcpStatuses.set(target, { kind: "connected", toolCount: notice.tools });
      } else if (notice.kind === "failed") {
        tab.mcpStatuses.set(target, { kind: "failed", reason: notice.reason });
      } else if (notice.kind === "disabled") {
        tab.mcpStatuses.set(target, { kind: "disabled" });
      }
      emitMcpSpecs(tab);
    });
    return runtime
      .reloadFromConfig(tab.runtime.loop)
      .then(() => undefined)
      .catch((err) => {
        emit({ type: "$error", message: `mcp bridge failed: ${(err as Error).message}` }, tab.id);
      });
  }

  /** Snapshot of every open tab — workspace dir, loaded session and focus, in tab order. Persisted after open/close/switch so a restart restores the full tab set and each conversation (issues #933, #1244). */
  function persistOpenTabs(): void {
    try {
      saveDesktopOpenTabs(
        Array.from(tabs.values()).map((t) => ({
          dir: t.rootDir,
          session: t.currentSession || undefined,
          active: t.id === lastActiveTabId,
        })),
      );
    } catch {
      // best-effort — disk / perms shouldn't break tab management
    }
  }

  async function closeTab(tab: Tab): Promise<void> {
    abortTurn(tab);
    try {
      await tab.toolset?.jobs.shutdown();
    } catch {
      // shutdown errors aren't actionable here
    }
    if (tab.mcpRuntime) {
      try {
        await tab.mcpRuntime.closeAll();
      } catch {
        // MCP shutdown errors aren't actionable here either
      }
    }
    tabs.delete(tab.id);
    if (first && first.id === tab.id) {
      const next = tabs.values().next().value;
      if (next) first = next;
    }
    persistOpenTabs();
    emit({ type: "$tab_closed" }, tab.id);
  }

  async function runTurn(tab: Tab, text: string, fromQQ = false): Promise<void> {
    if (!tab.runtime) return;
    const rt = tab.runtime;
    tab.aborter = new AbortController();
    if (fromQQ) markQQTurnStarted(qqRuntime.routing, tab.id);
    let lastAssistantText = "";
    if (tab.currentSession) {
      const existing = loadSessionMeta(tab.currentSession).summary;
      if (!existing || !existing.trim()) {
        const summary = text.replace(/\s+/g, " ").trim().slice(0, 60);
        if (summary) {
          try {
            patchSessionMeta(tab.currentSession, { summary });
          } catch {
            // meta is for display only — failure shouldn't block the turn
          }
        }
      }
    }
    if (tab.hooks.some((h) => h.event === "UserPromptSubmit")) {
      const report = await runHooks({
        hooks: tab.hooks,
        payload: { event: "UserPromptSubmit", cwd: tab.rootDir, prompt: text },
      });
      for (const o of report.outcomes) {
        if (o.decision === "pass") continue;
        emit({ type: "$error", message: formatHookOutcomeMessage(o) }, tab.id);
      }
      if (report.blocked) {
        tab.aborter = null;
        emit({ type: "$turn_complete" }, tab.id);
        if (fromQQ) markQQTurnFinished(qqRuntime.routing, tab.id);
        return;
      }
    }
    await tabContext.run(tab.id, async () => {
      try {
        let emittedTurnContext = false;
        for await (const ev of rt.loop.step(text)) {
          if (!emittedTurnContext) {
            emittedTurnContext = true;
            emitCtxBreakdown(tab);
          }
          if (ev.role === "assistant_final" && ev.content) {
            lastAssistantText = ev.content;
          }
          for (const kev of rt.eventizer.consume(ev, rt.ctx)) emit(kev, tab.id);
          if (ev.role === "assistant_final" || ev.role === "tool") {
            emitCtxBreakdown(tab);
          }
          // Memory tools mutate disk state behind the loop's back — the UI
          // panel won't know until we re-emit. Without this the right-hand
          // panel only updates on tab reopen.
          if (ev.role === "tool" && (ev.toolName === "remember" || ev.toolName === "forget")) {
            emitMemory(tab);
          }
          if (tab.aborter?.signal.aborted) break;
        }
      } catch (err) {
        emit({ type: "$error", message: (err as Error).message }, tab.id);
      } finally {
        tab.aborter = null;
        // If a session switch happened while this turn was running,
        // suppress stale events to avoid UI state corruption (#1217).
        if (!tab.switching) {
          if (
            fromQQ &&
            lastAssistantText &&
            qqRuntime.channel &&
            shouldRouteQQForTab(qqRuntime.routing, tab.id)
          ) {
            await qqRuntime.channel.sendResponse(lastAssistantText).catch((err) => {
              emit(
                { type: "$error", message: `qq send failed: ${(err as Error).message}` },
                tab.id,
              );
            });
          }
          emit({ type: "$turn_complete" }, tab.id);
          if (tab.planTotalSteps > 0 && tab.completedStepIds.size >= tab.planTotalSteps) {
            tab.completedStepIds.clear();
            tab.planTotalSteps = 0;
            emit({ type: "$plan_cleared" }, tab.id);
          }
          emitSessions(tab);
          void emitBalance(tab);
          if (tab.hooks.some((h) => h.event === "Stop")) {
            const stopReport = await runHooks({
              hooks: tab.hooks,
              payload: {
                event: "Stop",
                cwd: tab.rootDir,
                lastAssistantText,
                last_assistant_message: lastAssistantText,
                turn: rt.loop.stats.summary().turns,
              },
            });
            for (const o of stopReport.outcomes) {
              if (o.decision === "pass") continue;
              emit({ type: "$error", message: formatHookOutcomeMessage(o) }, tab.id);
            }
          }
        }
        if (fromQQ) markQQTurnFinished(qqRuntime.routing, tab.id);
        tab.switching = false;
      }
    });
  }

  async function switchWorkspace(tab: Tab, nextDir: string): Promise<void> {
    const target = resolve(nextDir);
    if (target === tab.rootDir) {
      emitSettings(tab);
      return;
    }
    if (!existsSync(target) || !statSync(target).isDirectory()) {
      emit({ type: "$error", message: `Workspace not found: ${target}` }, tab.id);
      emitSettings(tab);
      return;
    }
    abortTurn(tab);
    try {
      await tab.toolset?.jobs.shutdown();
    } catch {
      // shutdown errors aren't actionable here
    }
    tab.rootDir = target;
    saveWorkspaceDir(target);
    pushRecentWorkspace(target);
    tab.fileIndex = null;
    tab.fileIndexBuilding = null;
    tab.fileIndexBuiltAt = 0;
    tab.symbolIndex = null;
    tab.symbolBuilding = null;
    tab.recentMentions.length = 0;
    tab.hooks = loadHooks({ projectRoot: target });
    tab.currentSession = mintSessionFor(target);
    tab.toolset = await buildCodeToolset({
      rootDir: target,
      onSkillInstalled: () => emitSkills(tab),
      onJobsChanged: () => emitJobs(),
    });
    tab.system = codeSystemPrompt(target, {
      hasSemanticSearch: tab.toolset.semantic.enabled,
      modelId: tab.currentModel,
    });
    if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
    emitSessions(tab);
    emitSettings(tab);
    emitSkills(tab);
    persistOpenTabs();
  }

  function forgetGate(id: number): Tab | undefined {
    for (const t of tabs.values()) {
      if (t.pendingGateIds.delete(id)) return t;
    }
    return undefined;
  }

  function abortTurn(tab: Tab, opts: LoopAbortOptions = {}): void {
    tab.aborter?.abort();
    tab.runtime?.loop.abort(opts);
  }

  function tabSessionLabel(tab: Tab): string {
    if (tab.currentSession) {
      try {
        const summary = loadSessionMeta(tab.currentSession).summary?.trim();
        if (summary) return summary;
      } catch {
        // session file unreadable — fall through to workspace basename
      }
    }
    return tab.rootDir.split(/[\\/]/).filter(Boolean).pop() ?? tab.rootDir;
  }

  function emitJobs(): void {
    const items: JobInfoPayload[] = [];
    for (const t of tabs.values()) {
      const reg = t.toolset?.jobs;
      if (!reg) continue;
      const label = tabSessionLabel(t);
      for (const j of reg.list()) {
        items.push({
          id: j.id,
          tabId: t.id,
          sessionLabel: label,
          command: j.command,
          pid: j.pid,
          running: j.running,
          exitCode: j.exitCode,
          startedAt: j.startedAt,
          outputTail: tailLines(j.output, 8),
          spawnError: j.spawnError,
        });
      }
    }
    items.sort((a, b) => {
      if (a.running !== b.running) return a.running ? -1 : 1;
      return b.startedAt - a.startedAt;
    });
    emit({ type: "$jobs", items });
  }

  async function stopJob(jobId: number): Promise<boolean> {
    for (const t of tabs.values()) {
      const reg = t.toolset?.jobs;
      if (!reg) continue;
      const hit = reg.list().find((j) => j.id === jobId);
      if (!hit) continue;
      await reg.stop(jobId);
      return true;
    }
    return false;
  }

  async function stopAllJobs(): Promise<void> {
    const ops: Promise<unknown>[] = [];
    for (const t of tabs.values()) {
      const reg = t.toolset?.jobs;
      if (!reg) continue;
      for (const j of reg.list()) {
        if (j.running) ops.push(reg.stop(j.id));
      }
    }
    await Promise.allSettled(ops);
  }

  function cancelPendingGates(tab: Tab): void {
    const hadActivePlan = tab.planTotalSteps > 0 || tab.completedStepIds.size > 0;
    const ids = [...tab.pendingGateIds];
    tab.pendingGateIds.clear();
    for (const id of ids) pauseGate.cancel(id);
    if (hadActivePlan) {
      tab.completedStepIds.clear();
      tab.planTotalSteps = 0;
      emit({ type: "$plan_cleared" }, tab.id);
    }
  }

  // `first` is the fallback tab for legacy tabId-less RPC messages. We
  // assign it lazily below so saved-tabs restore (issue #933) can choose
  // the boot dir before construction, and rotate `first` to the next
  // surviving tab when its source closes.
  let shuttingDown = false;
  async function gracefulShutdown(): Promise<void> {
    if (shuttingDown) return;
    shuttingDown = true;
    await stopDesktopQQ(false).catch(() => undefined);
    await Promise.allSettled(
      [...tabs.values()].map((t) => t.toolset?.jobs.shutdown(1500) ?? Promise.resolve()),
    );
    process.exit(0);
  }
  process.on("SIGTERM", () => {
    void gracefulShutdown();
  });
  process.on("SIGINT", () => {
    void gracefulShutdown();
  });

  pauseGate.on((req) => {
    const tab = activeRunningTab();
    const tabId = tab?.id;
    if (tab) tab.pendingGateIds.add(req.id);
    // Shared auto-resolve policy (e.g. plan_checkpoint in auto/yolo) — must
    // still run BEFORE we emit any UI event, otherwise the surface flickers
    // a card that we'd immediately tear down.
    const auto = autoResolveVerdict(req, loadEditMode());
    if (auto !== null) {
      // plan_checkpoint specifically needs the step-completed signal to flow
      // through so the rail progress ticks. Emit it before resolving.
      if (req.kind === "plan_checkpoint") {
        const payload = req.payload as {
          stepId: string;
          title?: string;
          result: string;
          notes?: string;
        };
        if (tab) tab.completedStepIds.add(payload.stepId);
        emit(
          {
            type: "$step_completed",
            stepId: payload.stepId,
            title: payload.title,
            result: payload.result,
            notes: payload.notes,
          },
          tabId,
        );
      }
      if (tab) tab.pendingGateIds.delete(req.id);
      pauseGate.resolve(req.id, auto);
      return;
    }
    if (req.kind === "run_command" || req.kind === "run_background") {
      const payload = req.payload as {
        command?: string;
        cwd?: string;
        timeoutSec?: number;
        waitSec?: number;
      };
      if (tab) setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      emit(
        {
          type: "$confirm_required",
          id: req.id,
          kind: req.kind,
          command: payload.command ?? "",
          prompt: toApprovalPrompt({
            id: req.id,
            kind: req.kind,
            payload,
          }),
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    if (req.kind === "path_access") {
      const payload = req.payload as {
        path: string;
        intent: "read" | "write";
        toolName: string;
        sandboxRoot: string;
        allowPrefix: string;
      };
      if (tab) setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      emit(
        {
          type: "$path_access_required",
          id: req.id,
          path: payload.path,
          intent: payload.intent,
          toolName: payload.toolName,
          sandboxRoot: payload.sandboxRoot,
          allowPrefix: payload.allowPrefix,
          prompt: toApprovalPrompt({
            id: req.id,
            kind: req.kind,
            payload,
          }),
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    if (req.kind === "choice") {
      const payload = req.payload as {
        question: string;
        options: ChoiceOption[];
        allowCustom: boolean;
      };
      if (tab) setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      emit(
        {
          type: "$choice_required",
          id: req.id,
          question: payload.question,
          options: payload.options,
          allowCustom: payload.allowCustom,
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    if (req.kind === "plan_proposed") {
      const payload = req.payload as { plan: string; steps?: PlanStepLite[]; summary?: string };
      if (tab) {
        tab.completedStepIds.clear();
        tab.planTotalSteps = payload.steps?.length ?? 0;
        setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      }
      emit(
        {
          type: "$plan_required",
          id: req.id,
          plan: payload.plan,
          steps: payload.steps,
          summary: payload.summary,
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    if (req.kind === "plan_checkpoint") {
      const payload = req.payload as {
        stepId: string;
        title?: string;
        result: string;
        notes?: string;
      };
      if (tab) {
        tab.completedStepIds.add(payload.stepId);
        setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      }
      emit(
        {
          type: "$step_completed",
          stepId: payload.stepId,
          title: payload.title,
          result: payload.result,
          notes: payload.notes,
        },
        tabId,
      );
      emit(
        {
          type: "$checkpoint_required",
          id: req.id,
          stepId: payload.stepId,
          title: payload.title,
          result: payload.result,
          notes: payload.notes,
          completed: tab?.completedStepIds.size ?? 0,
          total: tab?.planTotalSteps ?? 0,
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    if (req.kind === "plan_revision") {
      const payload = req.payload as {
        reason: string;
        remainingSteps: PlanStepLite[];
        summary?: string;
      };
      if (tab) setQQPendingInteraction(qqRuntime.routing, tab.id, req.id, req.kind, payload);
      emit(
        {
          type: "$revision_required",
          id: req.id,
          reason: payload.reason,
          remainingSteps: payload.remainingSteps,
          summary: payload.summary,
        },
        tabId,
      );
      if (tab) handleQQPauseRequest(tab, req.kind, payload as Record<string, unknown>);
      return;
    }
    // Unknown PauseKind — `never` makes a new kind without a handler a compile
    // error; the runtime cancel is the last-mile defense so the agent loop
    // doesn't hang waiting on a request no one will resolve.
    const exhaustive: never = req.kind;
    process.stderr.write(
      `[desktop] no handler for pause kind "${String(exhaustive)}" — auto-cancelling gate id=${req.id}\n`,
    );
    if (tab) tab.pendingGateIds.delete(req.id);
    pauseGate.cancel(req.id);
  });

  // Fast-path: emit disk-only events immediately so the UI shell renders
  // before the toolset finishes building. Heavy work (semantic bootstrap,
  // MCP probes, runtime construction) runs in initTabToolset which fires
  // `$ready` when it completes — until then `state.ready` keeps the
  // composer disabled, so users can't send a message before the runtime
  // exists. emitBalance was already fire-and-forget.
  function bootstrapTab(
    initialDir?: string,
    restore?: { session?: string; active?: boolean },
  ): Tab {
    const tab = createTabSkeleton(initialDir);
    // Reopen the conversation the tab had, if its jsonl is still readable.
    let restoredMessages: LoadedMessage[] | undefined;
    if (restore?.session) {
      try {
        if (existsSync(sessionPath(restore.session))) {
          const msgs = buildLoadedMessages(loadSessionMessages(restore.session));
          if (msgs.length > 0) {
            tab.currentSession = restore.session;
            restoredMessages = msgs;
          }
        }
      } catch {
        // unreadable jsonl — fall back to the freshly minted session
      }
    }
    emit({ type: "$tab_opened", workspaceDir: tab.rootDir, active: restore?.active }, tab.id);
    emitSessions(tab);
    emitSettings(tab);
    emitMcpSpecs(tab);
    emitSkills(tab);
    emitMemory(tab);
    emitQQSettings(tab);
    if (restoredMessages) {
      const meta = loadSessionMeta(tab.currentSession);
      emit(
        {
          type: "$session_loaded",
          name: tab.currentSession,
          messages: restoredMessages,
          carryover: {
            totalCostUsd: meta.totalCostUsd ?? 0,
            cacheHitTokens: meta.cacheHitTokens ?? 0,
            cacheMissTokens: meta.cacheMissTokens ?? 0,
            totalCompletionTokens: meta.totalCompletionTokens ?? 0,
          },
        },
        tab.id,
      );
    }
    if (!loadApiKey()) emit({ type: "$needs_setup", reason: "no_api_key" }, tab.id);
    void emitBalance(tab);
    void initTabToolset(tab)
      .then(() => {
        if (loadApiKey()) emit({ type: "$ready" }, tab.id);
        emitCtxBreakdown(tab);
      })
      .catch((err) => {
        emit({ type: "$error", message: `init failed: ${(err as Error).message}` }, tab.id);
      });
    return tab;
  }

  // Restore the full tab set from the previous session — workspace dir,
  // loaded session and focused tab (issues #933, #1244). Missing dirs
  // are silently skipped — a deleted workspace shouldn't break boot.
  const savedTabs = loadDesktopOpenTabs().filter((t) => {
    try {
      return existsSync(t.dir) && statSync(t.dir).isDirectory();
    } catch {
      return false;
    }
  });
  // When launched with --dir, find the matching saved tab so the user's
  // previous session is restored automatically.
  const startupDir = opts.dir;
  const startupTab = startupDir
    ? savedTabs.find((t) => resolve(t.dir) === resolve(startupDir))
    : savedTabs[0];
  first = bootstrapTab(opts.dir ?? savedTabs[0]?.dir, startupTab);
  const restored: Tab[] = [first];
  for (const t of savedTabs.slice(1)) restored.push(bootstrapTab(t.dir, t));
  // Mirror the persisted focus so the next persist round-trips it.
  const activeIdx = savedTabs.findIndex((t) => t.active);
  lastActiveTabId = ((activeIdx >= 0 ? restored[activeIdx] : first) ?? first).id;
  persistOpenTabs();
  const qqConfig = loadQQConfig();
  if (qqConfig.enabled && qqConfig.appId && qqConfig.appSecret) {
    void startDesktopQQ(false).catch(() => undefined);
  } else {
    broadcastQQSettings();
  }

  const rl = createInterface({ input: stdin });
  rl.on("line", (line) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    let msg: InMessage;
    try {
      msg = JSON.parse(trimmed) as InMessage;
    } catch {
      emit({ type: "$error", message: `bad json on stdin: ${trimmed.slice(0, 80)}` });
      return;
    }

    if (msg.cmd === "tab_open") {
      try {
        // A user-opened tab takes focus.
        const opened = bootstrapTab(msg.workspaceDir, { active: true });
        lastActiveTabId = opened.id;
        persistOpenTabs();
      } catch (err) {
        emit({ type: "$error", message: `tab_open failed: ${(err as Error).message}` });
      }
      return;
    }
    if (msg.cmd === "tab_activate") {
      if (tabs.has(msg.tabId)) {
        lastActiveTabId = msg.tabId;
        persistOpenTabs();
      }
      return;
    }
    if (msg.cmd === "confirm_response") {
      forgetGate(msg.id);
      pauseGate.resolve(msg.id, msg.response);
      return;
    }
    if (msg.cmd === "choice_response") {
      forgetGate(msg.id);
      pauseGate.resolve(msg.id, msg.response);
      return;
    }
    if (msg.cmd === "plan_response") {
      const tab = forgetGate(msg.id);
      if (tab && msg.response.type === "cancel") {
        abortTurn(tab);
        tab.completedStepIds.clear();
        tab.planTotalSteps = 0;
        emit({ type: "$plan_cleared" }, tab.id);
      }
      pauseGate.resolve(msg.id, msg.response);
      return;
    }
    if (msg.cmd === "checkpoint_response") {
      const tab = forgetGate(msg.id);
      if (tab && msg.response.type === "stop") {
        tab.completedStepIds.clear();
        tab.planTotalSteps = 0;
        emit({ type: "$plan_cleared" }, tab.id);
      }
      pauseGate.resolve(msg.id, msg.response);
      return;
    }
    if (msg.cmd === "revision_response") {
      forgetGate(msg.id);
      pauseGate.resolve(msg.id, msg.response);
      return;
    }
    if (msg.cmd === "setup_save_key") {
      const key = msg.key.trim();
      if (!isPlausibleKey(key)) {
        emit({
          type: "$error",
          message: "Key looks too short — paste the full token (16+ chars, no spaces).",
        });
        return;
      }
      try {
        saveApiKey(key);
        bridgeEndpointEnv();
        for (const tab of tabs.values()) {
          // Skeleton tabs still mid-bootstrap pick up the new key inside
          // initTabToolset's tail when buildCodeToolset settles — don't
          // try to construct a runtime against a null toolset here.
          if (!tab.toolset) {
            emitSettings(tab);
            void emitBalance(tab);
            continue;
          }
          tab.runtime = buildRuntimeFor(tab);
          emit({ type: "$ready" }, tab.id);
          emitSettings(tab);
          void emitBalance(tab);
        }
      } catch (err) {
        emit({ type: "$error", message: `saveApiKey failed: ${(err as Error).message}` });
      }
      return;
    }

    if (msg.cmd === "desktop_resync") {
      // WebView reloads (DevTools F5, host-side respawn) leave the Node child
      // alive but the React app starts blank. Re-fire the bootstrap events
      // so it can rehydrate without restarting the agent.
      const hasKey = !!loadApiKey();
      for (const t of tabs.values()) {
        emit(
          { type: "$tab_opened", workspaceDir: t.rootDir, active: t.id === lastActiveTabId },
          t.id,
        );
        emitSessions(t);
        emitSettings(t);
        emitMcpSpecs(t);
        emitSkills(t);
        emitMemory(t);
        emitQQSettings(t);
        if (!hasKey) emit({ type: "$needs_setup", reason: "no_api_key" }, t.id);
        else if (t.toolset) emit({ type: "$ready" }, t.id);
        void emitBalance(t);
        // Re-emit session_loaded so the resumed session's messages and
        // usage stats (cost, tokens, cache%) are restored on the frontend.
        if (t.currentSession) {
          try {
            const msgs = buildLoadedMessages(loadSessionMessages(t.currentSession));
            const meta = loadSessionMeta(t.currentSession);
            emit(
              {
                type: "$session_loaded",
                name: t.currentSession,
                messages: msgs,
                carryover: {
                  totalCostUsd: meta.totalCostUsd ?? 0,
                  cacheHitTokens: meta.cacheHitTokens ?? 0,
                  cacheMissTokens: meta.cacheMissTokens ?? 0,
                  totalCompletionTokens: meta.totalCompletionTokens ?? 0,
                },
              },
              t.id,
            );
          } catch {
            // unreadable jsonl — skip re-emit
          }
        }
        emitCtxBreakdown(t);
      }
      return;
    }
    if (msg.cmd === "jobs_list") {
      emitJobs();
      return;
    }
    if (msg.cmd === "jobs_stop") {
      void stopJob(msg.jobId).finally(() => emitJobs());
      return;
    }
    if (msg.cmd === "jobs_stop_all") {
      void stopAllJobs().finally(() => emitJobs());
      return;
    }

    const tab = msg.tabId ? tabs.get(msg.tabId) : first;
    if (!tab) {
      // No tabId on the emit ⇒ the renderer's per-tab router drops it
      // silently. Surface to stderr instead so it's at least visible
      // when the desktop is launched from a terminal.
      process.stderr.write(
        `rpc dispatch: unknown tabId=${msg.tabId} for cmd=${msg.cmd} — dropping\n`,
      );
      return;
    }

    if (msg.cmd === "abort") {
      abortTurn(tab, desktopUserAbortLoopOptions());
      cancelPendingGates(tab);
      return;
    }
    if (msg.cmd === "tab_close") {
      void closeTab(tab);
      return;
    }
    if (msg.cmd === "mcp_specs_get") {
      emitMcpSpecs(tab);
      return;
    }
    if (msg.cmd === "mcp_specs_add") {
      const spec = msg.spec.trim();
      if (!spec) {
        emit({ type: "$error", message: "mcp_specs_add: spec is empty" }, tab.id);
        return;
      }
      let parsedSpec: ReturnType<typeof parseMcpSpec> | null = null;
      try {
        parsedSpec = parseMcpSpec(spec);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_add: ${(err as Error).message}` }, tab.id);
        return;
      }
      try {
        const cfg = readConfig();
        if (parsedSpec?.name && cfg.mcpServers?.[parsedSpec.name]) {
          emit(
            {
              type: "$error",
              message: `mcp_specs_add: ${parsedSpec.name} already exists in canonical mcpServers config`,
            },
            tab.id,
          );
          return;
        }
        const list = cfg.mcp ?? [];
        if (!list.includes(spec)) {
          cfg.mcp = [...list, spec];
          writeConfig(cfg);
        }
        emitMcpSpecs(tab);
        void bridgeTabMcp(tab);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_add: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "mcp_specs_remove") {
      try {
        const cfg = readConfig();
        let changed = false;
        if (Array.isArray(cfg.mcp) && cfg.mcp.includes(msg.spec)) {
          cfg.mcp = cfg.mcp.filter((s) => s !== msg.spec);
          if (cfg.mcp.length === 0) cfg.mcp = undefined;
          changed = true;
        }
        try {
          const parsed = parseMcpSpec(msg.spec);
          if (parsed.name) {
            if (cfg.mcpServers?.[parsed.name]) {
              delete cfg.mcpServers[parsed.name];
              if (Object.keys(cfg.mcpServers).length === 0) cfg.mcpServers = undefined;
              changed = true;
            }
            stripLegacyMcpConfigForNames(cfg, new Set([parsed.name]));
          }
        } catch {
          /* ignore parse failure during removal — raw legacy match above is enough */
        }
        if (changed) writeConfig(cfg);
        tab.mcpStatuses.delete(msg.spec);
        emitMcpSpecs(tab);
        void bridgeTabMcp(tab);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_remove: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "mcp_import_servers") {
      try {
        const cfg = readConfig();
        const { forceSpecs } = applyImportedMcpServersToConfig(cfg, msg.servers);
        writeConfig(cfg);
        emitMcpSpecs(tab);
        void bridgeTabMcp(tab, { forceSpecs });
      } catch (err) {
        emit({ type: "$error", message: `mcp_import_servers: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "mcp_specs_update") {
      try {
        const cfg = readConfig();
        const { updatedRaw, forceSpecs } = applyMcpSpecUpdateToConfig(cfg, msg.raw, msg.server);
        writeConfig(cfg);
        tab.mcpStatuses.delete(msg.raw);
        if (updatedRaw) tab.mcpStatuses.delete(updatedRaw);
        emitMcpSpecs(tab);
        void bridgeTabMcp(tab, { forceSpecs });
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_update: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "mcp_specs_retry") {
      try {
        tab.mcpStatuses.delete(msg.raw);
        emitMcpSpecs(tab);
        void bridgeTabMcp(tab);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_retry: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "skills_get") {
      emitSkills(tab);
      return;
    }
    if (msg.cmd === "skill_run") {
      if (!tab.runtime) {
        emit(
          { type: "$error", message: "Not configured yet — paste your DeepSeek API key first." },
          tab.id,
        );
        return;
      }
      try {
        const payload = buildSkillPayload(tab, msg.name, msg.args);
        if (!payload) {
          emit({ type: "$error", message: `skill not found: ${msg.name}` }, tab.id);
          return;
        }
        void runTurn(tab, payload);
      } catch (err) {
        emit({ type: "$error", message: `skill_run: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "session_list") {
      emitSessions(tab);
      return;
    }
    if (msg.cmd === "session_delete") {
      deleteSession(msg.name);
      if (tab.currentSession === msg.name) {
        startNewChatInTab(tab);
        emit(
          {
            type: "$session_loaded",
            name: tab.currentSession,
            messages: [],
            carryover: {
              totalCostUsd: 0,
              cacheHitTokens: 0,
              cacheMissTokens: 0,
              totalCompletionTokens: 0,
            },
          },
          tab.id,
        );
        emitCtxBreakdown(tab);
      } else {
        emitSessions(tab);
      }
      return;
    }
    if (msg.cmd === "session_rename") {
      try {
        const trimmed = normalizeSessionTitle(msg.title);
        patchSessionMeta(msg.name, { summary: trimmed || undefined });
        emitSessions(tab);
      } catch (err) {
        emit(
          { type: "$error", message: `session_rename failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "session_import") {
      try {
        const result = importExternalSession({
          source: msg.source,
          path: msg.path,
          name: msg.name,
          workspace: tab.rootDir,
        });
        emitSessions(tab);
        loadSessionIntoTab(tab, result.name, {
          abortTurn,
          cancelPendingGates,
          persistOpenTabs,
        });
      } catch (err) {
        emit(
          { type: "$error", message: `session_import failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "session_import_scan") {
      try {
        emit({ type: "$session_import_sources", apps: discoverExternalSessionApps() }, tab.id);
      } catch (err) {
        emit(
          { type: "$error", message: `session_import_scan failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "session_import_bulk") {
      try {
        const result = importExternalSessions({
          sources: msg.sources,
          workspace: tab.rootDir,
        });
        emitSessions(tab);
        emit(
          {
            type: "$session_import_result",
            imported: result.imported,
            skipped: result.skipped,
            failed: result.failed,
          },
          tab.id,
        );
        if (result.latestName) {
          loadSessionIntoTab(tab, result.latestName, {
            abortTurn,
            cancelPendingGates,
            persistOpenTabs,
          });
        }
      } catch (err) {
        emit(
          { type: "$error", message: `session_import_bulk failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "session_load") {
      try {
        loadSessionIntoTab(tab, msg.name, {
          abortTurn,
          cancelPendingGates,
          persistOpenTabs,
        });
      } catch (err) {
        process.stderr.write(`session_load: "${msg.name}" threw — ${(err as Error).message}\n`);
        emit({ type: "$error", message: `session_load failed: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "memory_read") {
      try {
        const detail = readMemoryEntryDetail({ path: msg.path }, tab.rootDir);
        emit({ type: "$memory_detail", detail }, tab.id);
      } catch (err) {
        emit({ type: "$error", message: `memory_read failed: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "new_chat") {
      // Only set switching flag when there's a live turn to abort —
      // otherwise the flag stays true and suppresses the first turn's events (#1217).
      startNewChatInTab(tab);
      return;
    }
    if (msg.cmd === "settings_get") {
      emitSettings(tab);
      return;
    }
    if (msg.cmd === "qq_status_get") {
      emitQQSettings(tab);
      return;
    }
    if (msg.cmd === "settings_save") {
      try {
        if (msg.reasoningEffort !== undefined && isReasoningEffort(msg.reasoningEffort)) {
          saveReasoningEffort(msg.reasoningEffort);
          tab.runtime?.loop.configure({ reasoningEffort: msg.reasoningEffort });
        }
        if (msg.editMode !== undefined) {
          saveEditMode(msg.editMode);
          if (tab.toolset) applyPlanMode(tab.toolset.tools, msg.editMode);
        }
        if (msg.budgetUsd !== undefined) {
          tab.budgetUsd = msg.budgetUsd ?? undefined;
          tab.runtime?.loop.setBudget(msg.budgetUsd);
        }
        if (msg.baseUrl !== undefined) saveBaseUrl(msg.baseUrl);
        if (msg.workspaceDir !== undefined) {
          void switchWorkspace(tab, msg.workspaceDir);
          return;
        }
        if (msg.recentWorkspaces !== undefined) {
          const cfg = readConfig();
          cfg.recentWorkspaces = msg.recentWorkspaces;
          writeConfig(cfg);
        }
        if (msg.editor !== undefined) saveEditor(msg.editor);
        if (
          msg.desktopCloseBehavior === "closeToTray" ||
          msg.desktopCloseBehavior === "closeToQuit"
        ) {
          saveDesktopCloseBehavior(msg.desktopCloseBehavior);
        }
        if (msg.showSystemEvents !== undefined) saveShowSystemEvents(msg.showSystemEvents);
        if (
          msg.webSearchEngine !== undefined ||
          msg.webSearchEndpoint !== undefined ||
          msg.metasoApiKey !== undefined ||
          msg.baiduApiKey !== undefined ||
          msg.tavilyApiKey !== undefined ||
          msg.perplexityApiKey !== undefined ||
          msg.exaApiKey !== undefined ||
          msg.ollamaApiKey !== undefined ||
          msg.braveApiKey !== undefined
        ) {
          const cfg = readConfig();
          if (msg.webSearchEngine !== undefined) cfg.webSearchEngine = msg.webSearchEngine;
          if (msg.webSearchEndpoint !== undefined) {
            cfg.webSearchEndpoint = msg.webSearchEndpoint?.trim() || undefined;
          }
          if (msg.metasoApiKey !== undefined) {
            cfg.metasoApiKey = msg.metasoApiKey?.trim() || undefined;
          }
          if (msg.baiduApiKey !== undefined) {
            cfg.baiduApiKey = msg.baiduApiKey?.trim() || undefined;
          }
          if (msg.tavilyApiKey !== undefined) {
            cfg.tavilyApiKey = msg.tavilyApiKey?.trim() || undefined;
          }
          if (msg.perplexityApiKey !== undefined) {
            cfg.perplexityApiKey = msg.perplexityApiKey?.trim() || undefined;
          }
          if (msg.exaApiKey !== undefined) {
            cfg.exaApiKey = msg.exaApiKey?.trim() || undefined;
          }
          if (msg.ollamaApiKey !== undefined) {
            cfg.ollamaApiKey = msg.ollamaApiKey?.trim() || undefined;
          }
          if (msg.braveApiKey !== undefined) {
            cfg.braveApiKey = msg.braveApiKey?.trim() || undefined;
          }
          writeConfig(cfg);
        }
        if (msg.subagentModels !== undefined) {
          saveSubagentModels(msg.subagentModels);
          emitSkills(tab);
        }
        if (msg.contextTokens !== undefined) {
          const cfg = readConfig();
          cfg.contextTokens = msg.contextTokens;
          writeConfig(cfg);
        }
        if (msg.model !== undefined) {
          const next = msg.model.trim();
          if (next) {
            tab.currentModel = next;
            saveModel(next);
            if (tab.toolset) {
              tab.system = codeSystemPrompt(tab.rootDir, {
                hasSemanticSearch: tab.toolset.semantic.enabled,
                modelId: tab.currentModel,
              });
              if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
            }
          }
        }
        emitSettings(tab);
      } catch (err) {
        emit(
          { type: "$error", message: `settings_save failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "qq_config_save") {
      try {
        saveDesktopQQSettings(
          {
            appId: msg.appId,
            appSecret: msg.appSecret,
            sandbox: msg.sandbox,
          },
          undefined,
        );
        emitQQSettings(tab);
      } catch (err) {
        emit(
          { type: "$error", message: `qq_config_save failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "qq_connect") {
      try {
        const current = loadQQConfig();
        emit(
          {
            type: "status",
            id: Date.now(),
            ts: new Date().toISOString(),
            turn: 0,
            text: `QQ connecting (${current.sandbox ? "sandbox" : "production"})`,
          },
          tab.id,
        );
        void startDesktopQQ(true).then(
          () => {
            emit(
              {
                type: "status",
                id: Date.now(),
                ts: new Date().toISOString(),
                turn: 0,
                text: `QQ connected (${current.sandbox ? "sandbox" : "production"})`,
              },
              tab.id,
            );
            emitQQSettings(tab);
          },
          (err) => {
            emit(
              { type: "$error", message: `qq_connect failed: ${(err as Error).message}` },
              tab.id,
            );
            emitQQSettings(tab);
          },
        );
      } catch (err) {
        emit({ type: "$error", message: `qq_connect failed: ${(err as Error).message}` }, tab.id);
        emitQQSettings(tab);
      }
      return;
    }
    if (msg.cmd === "qq_disconnect") {
      try {
        void stopDesktopQQ(true).then(
          () => {
            emit(
              {
                type: "status",
                id: Date.now(),
                ts: new Date().toISOString(),
                turn: 0,
                text: "QQ disabled",
              },
              tab.id,
            );
            emitQQSettings(tab);
          },
          (err) => {
            emit(
              { type: "$error", message: `qq_disconnect failed: ${(err as Error).message}` },
              tab.id,
            );
            emitQQSettings(tab);
          },
        );
      } catch (err) {
        emit(
          { type: "$error", message: `qq_disconnect failed: ${(err as Error).message}` },
          tab.id,
        );
        emitQQSettings(tab);
      }
      return;
    }
    if (msg.cmd === "prompt_history_step") {
      try {
        const entry = promptHistoryStep({
          direction: msg.direction,
          cursor: msg.cursor ?? null,
          startSessionName: msg.startSessionName,
          stopSessionName: msg.stopSessionName,
          workspace: tab.rootDir,
        });
        emit({ type: "$prompt_history_result", nonce: msg.nonce, entry }, tab.id);
      } catch (err) {
        emit(
          { type: "$error", message: `prompt_history_step failed: ${(err as Error).message}` },
          tab.id,
        );
      }
      return;
    }
    if (msg.cmd === "mention_query") {
      const nonce = msg.nonce;
      const query = msg.query;
      const parsed = parseAtQuery(query);
      // Empty query → list workspace root's top-level entries (tree
      // style). Without this, bare `@` floods with all 5000 files; the
      // TUI's @+Tab pattern already shows the tree top.
      const treeWalk = parsed.trailingSlash || query.length === 0;
      if (treeWalk) {
        void listDirectory(tab.rootDir, parsed.dir)
          .then((entries) => {
            const results = entries.map((e) => (e.isDir ? `${e.path}/` : e.path));
            emit({ type: "$mention_results", nonce, query, results }, tab.id);
          })
          .catch((err) => {
            emit(
              { type: "$error", message: `mention_query (dir) failed: ${(err as Error).message}` },
              tab.id,
            );
            emit({ type: "$mention_results", nonce, query, results: [] }, tab.id);
          });
        return;
      }
      const wantSymbols = query.length >= 2 && !query.includes("/");
      void (async () => {
        try {
          const files = await getFileIndexFor(tab);
          const fileResults = rankPickerCandidates(files, query, {
            limit: wantSymbols ? 19 : 25,
            recentlyUsed: tab.recentMentions,
          });
          let symResults: string[] = [];
          if (wantSymbols) {
            const syms = await getSymbolIndexFor(tab);
            symResults = rankSymbols(syms, query, 6);
          }
          emit(
            { type: "$mention_results", nonce, query, results: [...symResults, ...fileResults] },
            tab.id,
          );
        } catch (err) {
          emit(
            { type: "$error", message: `mention_query failed: ${(err as Error).message}` },
            tab.id,
          );
          emit({ type: "$mention_results", nonce, query, results: [] }, tab.id);
        }
      })();
      return;
    }
    if (msg.cmd === "mention_picked") {
      pushMentionRecent(tab, msg.path);
      return;
    }
    if (msg.cmd === "mention_preview") {
      const nonce = msg.nonce;
      const rel = msg.path;
      const abs = isAbsolute(rel) ? rel : join(tab.rootDir, rel);
      const safeAbs = resolve(abs);
      const safeRoot = resolve(tab.rootDir);
      if (!safeAbs.startsWith(safeRoot)) {
        emit({ type: "$mention_preview", nonce, path: rel, head: "", totalLines: 0 }, tab.id);
        return;
      }
      void readFile(safeAbs, "utf8")
        .then((text) => {
          const lines = text.split(/\r?\n/);
          if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
          const head = lines.slice(0, 12).join("\n");
          emit(
            { type: "$mention_preview", nonce, path: rel, head, totalLines: lines.length },
            tab.id,
          );
        })
        .catch(() => {
          emit({ type: "$mention_preview", nonce, path: rel, head: "", totalLines: 0 }, tab.id);
        });
      return;
    }
    if (msg.cmd === "compact_history") {
      if (!tab.runtime) return;
      void tab.runtime.loop
        .compactHistory()
        .then(() => emitCtxBreakdown(tab))
        .catch((err: Error) => {
          emit({ type: "$error", message: `/compact failed: ${err.message}` }, tab.id);
        });
      return;
    }
    if (msg.cmd === "retry") {
      if (!tab.runtime) return;
      const prev = tab.runtime.loop.retryLastUser();
      if (prev) {
        emit({ type: "$retry_result", text: prev }, tab.id);
      }
      return;
    }
    if (msg.cmd === "btw") {
      if (!tab.runtime) return;
      const question = msg.text.trim();
      if (!question) return;
      runBtwOnTab(tab, question);
      return;
    }
    if (msg.cmd === "user_input") {
      if (!tab.runtime) {
        emit(
          { type: "$error", message: "Not configured yet — paste your DeepSeek API key first." },
          tab.id,
        );
        return;
      }
      void runTurn(tab, msg.text);
    }
  });

  await new Promise<void>((resolve) => {
    rl.on("close", () => {
      void gracefulShutdown();
      resolve();
    });
  });
}
