import { AsyncLocalStorage } from "node:async_hooks";
import { existsSync, statSync, writeSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { isAbsolute, join, resolve } from "node:path";
import { stdin } from "node:process";
import { createInterface } from "node:readline";
import {
  type FileWithStats,
  listDirectory,
  listFilesWithStatsAsync,
  parseAtQuery,
  rankPickerCandidates,
} from "../../at-mentions.js";
import { pickPrimaryBalance } from "../../client.js";
import { codeSystemPrompt } from "../../code/prompt.js";
import { buildCodeToolset } from "../../code/setup.js";
import {
  type EditMode,
  isPlausibleKey,
  loadApiKey,
  loadBaseUrl,
  loadEditMode,
  loadEditor,
  loadPreset,
  loadReasoningEffort,
  loadRecentWorkspaces,
  loadWorkspaceDir,
  pushRecentWorkspace,
  readConfig,
  saveApiKey,
  saveBaseUrl,
  saveEditMode,
  saveEditor,
  savePreset,
  saveReasoningEffort,
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
import { loadDotenv } from "../../env.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../../index.js";
import { parseMcpSpec } from "../../mcp/spec.js";
import {
  deleteSession,
  listSessionsForWorkspace,
  loadSessionMessages,
  loadSessionMeta,
  patchSessionMeta,
  timestampSuffix,
} from "../../memory/session.js";
import { SkillStore } from "../../skills.js";
import { countTokens } from "../../tokenizer.js";
import type { ChoiceOption } from "../../tools/choice.js";
import type { ChatMessage } from "../../types.js";
import { VERSION } from "../../version.js";
import { canonicalPresetName, resolvePreset } from "../ui/presets.js";

export interface DesktopOptions {
  model: string;
  budgetUsd?: number;
  /** Root directory the agent's filesystem tools operate inside. Defaults to cwd. */
  dir?: string;
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
  | { cmd: "new_chat" }
  | { cmd: "setup_save_key"; key: string }
  | { cmd: "settings_get" }
  | {
      cmd: "settings_save";
      reasoningEffort?: "high" | "max";
      editMode?: EditMode;
      budgetUsd?: number | null;
      baseUrl?: string;
      workspaceDir?: string;
      preset?: "auto" | "flash" | "pro";
      editor?: string;
    }
  | { cmd: "mention_query"; query: string; nonce: number }
  | { cmd: "mention_preview"; path: string; nonce: number }
  | { cmd: "mention_picked"; path: string }
  | { cmd: "tab_open"; workspaceDir?: string }
  | { cmd: "tab_close" }
  | { cmd: "mcp_specs_get" }
  | { cmd: "mcp_specs_add"; spec: string }
  | { cmd: "mcp_specs_remove"; spec: string }
  | { cmd: "skills_get" }
);

interface NeedsSetupEvent {
  type: "$needs_setup";
  reason: "no_api_key";
}

interface SettingsEvent {
  type: "$settings";
  reasoningEffort: "high" | "max";
  editMode: EditMode;
  budgetUsd: number | null;
  baseUrl?: string;
  apiKeyPrefix?: string;
  workspaceDir: string;
  recentWorkspaces: string[];
  model: string;
  preset: "auto" | "flash" | "pro";
  editor?: string;
  version: string;
}

interface BalanceEvent {
  type: "$balance";
  currency: string;
  total: number;
  isAvailable: boolean;
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
  items: { name: string; messageCount: number; mtime: string }[];
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

interface TabOpenedEvent {
  type: "$tab_opened";
  workspaceDir: string;
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
  };
}

interface ConfirmRequiredEvent {
  type: "$confirm_required";
  id: number;
  kind: "run_command" | "run_background";
  command: string;
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

interface McpSpecInfo {
  raw: string;
  name: string | null;
  transport: "stdio" | "sse" | "streamable-http";
  summary: string;
  parseError?: string;
}

interface McpSpecsEvent {
  type: "$mcp_specs";
  specs: McpSpecInfo[];
  bridged: boolean;
}

interface CtxBreakdownEvent {
  type: "$ctx_breakdown";
  reservedTokens: number;
}

interface SkillInfo {
  name: string;
  description: string;
  scope: "project" | "global" | "builtin";
  path: string;
  runAs: "inline" | "subagent";
  model?: string;
}

interface SkillsEvent {
  type: "$skills";
  items: SkillInfo[];
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
  | ChoiceRequiredEvent
  | PlanRequiredEvent
  | CheckpointRequiredEvent
  | RevisionRequiredEvent
  | StepCompletedEvent
  | PlanClearedEvent
  | SessionsEvent
  | SessionLoadedEvent
  | NeedsSetupEvent
  | SettingsEvent
  | BalanceEvent
  | MentionResultsEvent
  | MentionPreviewEvent
  | TabOpenedEvent
  | TabClosedEvent
  | McpSpecsEvent
  | SkillsEvent
  | CtxBreakdownEvent;

function emit(ev: EmittableEvent, tabId?: string): void {
  const payload = tabId ? { ...ev, tabId } : ev;
  writeSync(1, Buffer.from(`${JSON.stringify(payload)}\n`, "utf8"));
}

function buildLoadedMessages(records: ChatMessage[]): LoadedMessage[] {
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
  return out;
}

function emitSettings(tab: Tab): void {
  const apiKey = loadApiKey();
  const recent = loadRecentWorkspaces().filter((p) => p !== tab.rootDir);
  emit(
    {
      type: "$settings",
      reasoningEffort: loadReasoningEffort(),
      editMode: loadEditMode(),
      budgetUsd: tab.runtime?.loop.budgetUsd ?? null,
      baseUrl: loadBaseUrl(),
      apiKeyPrefix: apiKey ? `${apiKey.slice(0, 6)}…${apiKey.slice(-3)}` : undefined,
      workspaceDir: tab.rootDir,
      recentWorkspaces: recent,
      model: tab.currentModel,
      preset: tab.currentPreset,
      editor: loadEditor(),
      version: VERSION,
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
  emit(
    {
      type: "$balance",
      currency: primary.currency,
      total: Number(primary.total_balance),
      isAvailable: bal.is_available,
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
    }));
    emit({ type: "$sessions", items }, tab.id);
  } catch (err) {
    emit({ type: "$error", message: `session_list failed: ${(err as Error).message}` }, tab.id);
  }
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
      };
    }
    return {
      raw,
      name: parsed.name,
      transport: parsed.transport,
      summary: `${parsed.transport} · ${parsed.url}`,
    };
  } catch (err) {
    return {
      raw,
      name: null,
      transport: "stdio",
      summary: raw,
      parseError: (err as Error).message,
    };
  }
}

function emitMcpSpecs(tab: Tab): void {
  const cfg = readConfig();
  const specs = (cfg.mcp ?? []).map(summarizeMcpSpec);
  emit({ type: "$mcp_specs", specs, bridged: false }, tab.id);
}

// reserved = system prompt + tool specs, constant for the tab's lifetime once
// the loop is built. The growing log portion is already covered by the
// per-turn cache hit/miss numbers in `model.final`.
function emitCtxBreakdown(tab: Tab): void {
  if (!tab.runtime) return;
  try {
    const sys = countTokens(tab.runtime.loop.prefix.system);
    const tools = countTokens(JSON.stringify(tab.runtime.loop.prefix.toolSpecs));
    emit({ type: "$ctx_breakdown", reservedTokens: sys + tools }, tab.id);
  } catch {
    // tokenizer warmup can throw on first call before the data file loads
  }
}

function emitSkills(tab: Tab): void {
  try {
    const store = new SkillStore({ projectRoot: tab.rootDir });
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
  ctx: { model: string; prefixHash: string; reasoningEffort: "high" | "max" };
}

type SymbolEntry = { name: string; path: string; line: number; kind: string };

interface Tab {
  readonly id: string;
  rootDir: string;
  currentSession: string;
  currentPreset: "auto" | "flash" | "pro";
  currentModel: string;
  budgetUsd: number | undefined;
  toolset: Awaited<ReturnType<typeof buildCodeToolset>>;
  system: string;
  runtime: RuntimeState | null;
  aborter: AbortController | null;
  fileIndex: FileWithStats[] | null;
  fileIndexBuilding: Promise<FileWithStats[]> | null;
  symbolIndex: SymbolEntry[] | null;
  symbolBuilding: Promise<SymbolEntry[]> | null;
  recentMentions: string[];
  /** Pause-gate ids waiting on this tab — abort uses these to free stranded plan_checkpoint / plan_revision / shell-confirm callers. */
  pendingGateIds: Set<number>;
  /** Step ids already marked complete in the in-flight plan — also tells UI when a plan is "active". */
  completedStepIds: Set<string>;
  /** Total steps in the in-flight plan (0 = no active plan / steps not provided). */
  planTotalSteps: number;
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
  const client = new DeepSeekClient({ baseUrl: loadBaseUrl() });
  const prefix = new ImmutablePrefix({ system: tab.system, toolSpecs: tab.toolset.tools.specs() });
  const loop = new CacheFirstLoop({
    client,
    prefix,
    tools: tab.toolset.tools,
    model: tab.currentModel,
    budgetUsd: tab.budgetUsd,
    session: tab.currentSession,
  });
  const reasoningEffort = loadReasoningEffort();
  const eventizer = new Eventizer();
  const ctx = { model: tab.currentModel, prefixHash: prefix.fingerprint, reasoningEffort };
  return { loop, eventizer, ctx };
}

const TS_EXPORT_RE =
  /^export\s+(?:default\s+)?(?:async\s+)?(function|class|const|let|var|interface|type|enum)\s+\*?\s*(\w+)/;

async function getFileIndexFor(tab: Tab): Promise<FileWithStats[]> {
  if (tab.fileIndex) return tab.fileIndex;
  if (tab.fileIndexBuilding) return tab.fileIndexBuilding;
  tab.fileIndexBuilding = listFilesWithStatsAsync(tab.rootDir, { maxResults: 5000 })
    .then((res) => {
      tab.fileIndex = res;
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

export async function desktopCommand(opts: DesktopOptions): Promise<void> {
  loadDotenv();

  const tabs = new Map<string, Tab>();
  const tabContext = new AsyncLocalStorage<string>();

  function activeRunningTab(): Tab | undefined {
    const id = tabContext.getStore();
    return id ? tabs.get(id) : undefined;
  }

  async function createTab(initialDir?: string): Promise<Tab> {
    const dir = resolve(initialDir ?? opts.dir ?? loadWorkspaceDir() ?? process.cwd());
    pushRecentWorkspace(dir);
    const preset = canonicalPresetName(loadPreset());
    const resolved = resolvePreset(preset);
    const model = opts.model || resolved.model;
    const toolset = await buildCodeToolset({ rootDir: dir });
    const system = codeSystemPrompt(dir, {
      hasSemanticSearch: toolset.semantic.enabled,
      modelId: model,
    });
    const tab: Tab = {
      id: nextTabId(),
      rootDir: dir,
      currentSession: "",
      currentPreset: preset,
      currentModel: model,
      budgetUsd: opts.budgetUsd,
      toolset,
      system,
      runtime: null,
      aborter: null,
      fileIndex: null,
      fileIndexBuilding: null,
      symbolIndex: null,
      symbolBuilding: null,
      recentMentions: [],
      pendingGateIds: new Set<number>(),
      completedStepIds: new Set<string>(),
      planTotalSteps: 0,
    };
    tab.currentSession = mintSessionFor(dir);
    if (loadApiKey()) {
      process.env.DEEPSEEK_API_KEY = loadApiKey();
      tab.runtime = buildRuntimeFor(tab);
    }
    tabs.set(tab.id, tab);
    return tab;
  }

  async function closeTab(tab: Tab): Promise<void> {
    tab.aborter?.abort();
    try {
      await tab.toolset.jobs.shutdown();
    } catch {
      // shutdown errors aren't actionable here
    }
    tabs.delete(tab.id);
    emit({ type: "$tab_closed" }, tab.id);
  }

  async function runTurn(tab: Tab, text: string): Promise<void> {
    if (!tab.runtime) return;
    const rt = tab.runtime;
    tab.aborter = new AbortController();
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
    await tabContext.run(tab.id, async () => {
      try {
        for await (const ev of rt.loop.step(text)) {
          for (const kev of rt.eventizer.consume(ev, rt.ctx)) emit(kev, tab.id);
          if (tab.aborter?.signal.aborted) break;
        }
      } catch (err) {
        emit({ type: "$error", message: (err as Error).message }, tab.id);
      } finally {
        tab.aborter = null;
        emit({ type: "$turn_complete" }, tab.id);
        if (tab.planTotalSteps > 0 && tab.completedStepIds.size >= tab.planTotalSteps) {
          tab.completedStepIds.clear();
          tab.planTotalSteps = 0;
          emit({ type: "$plan_cleared" }, tab.id);
        }
        emitSessions(tab);
        void emitBalance(tab);
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
    tab.aborter?.abort();
    try {
      await tab.toolset.jobs.shutdown();
    } catch {
      // shutdown errors aren't actionable here
    }
    tab.rootDir = target;
    saveWorkspaceDir(target);
    pushRecentWorkspace(target);
    tab.fileIndex = null;
    tab.fileIndexBuilding = null;
    tab.symbolIndex = null;
    tab.symbolBuilding = null;
    tab.recentMentions.length = 0;
    tab.currentSession = mintSessionFor(target);
    tab.toolset = await buildCodeToolset({ rootDir: target });
    tab.system = codeSystemPrompt(target, {
      hasSemanticSearch: tab.toolset.semantic.enabled,
      modelId: tab.currentModel,
    });
    if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
    emitSessions(tab);
    emitSettings(tab);
    emitSkills(tab);
  }

  function forgetGate(id: number): Tab | undefined {
    for (const t of tabs.values()) {
      if (t.pendingGateIds.delete(id)) return t;
    }
    return undefined;
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

  const first = await createTab();
  process.once("exit", () => {
    for (const t of tabs.values()) void t.toolset.jobs.shutdown();
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
      const payload = req.payload as { command?: string };
      emit(
        { type: "$confirm_required", id: req.id, kind: req.kind, command: payload.command ?? "" },
        tabId,
      );
      return;
    }
    if (req.kind === "choice") {
      const payload = req.payload as {
        question: string;
        options: ChoiceOption[];
        allowCustom: boolean;
      };
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
      return;
    }
    if (req.kind === "plan_proposed") {
      const payload = req.payload as { plan: string; steps?: PlanStepLite[]; summary?: string };
      if (tab) {
        tab.completedStepIds.clear();
        tab.planTotalSteps = payload.steps?.length ?? 0;
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
      return;
    }
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
      return;
    }
    if (req.kind === "plan_revision") {
      const payload = req.payload as {
        reason: string;
        remainingSteps: PlanStepLite[];
        summary?: string;
      };
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
      return;
    }
  });

  emit({ type: "$tab_opened", workspaceDir: first.rootDir }, first.id);
  if (loadApiKey()) emit({ type: "$ready" }, first.id);
  else emit({ type: "$needs_setup", reason: "no_api_key" }, first.id);
  emitSessions(first);
  emitSettings(first);
  emitMcpSpecs(first);
  emitSkills(first);
  emitCtxBreakdown(first);
  void emitBalance(first);

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
      void (async () => {
        try {
          const tab = await createTab(msg.workspaceDir);
          emit({ type: "$tab_opened", workspaceDir: tab.rootDir }, tab.id);
          if (loadApiKey()) emit({ type: "$ready" }, tab.id);
          else emit({ type: "$needs_setup", reason: "no_api_key" }, tab.id);
          emitSessions(tab);
          emitSettings(tab);
          emitMcpSpecs(tab);
          emitSkills(tab);
          emitCtxBreakdown(tab);
          void emitBalance(tab);
        } catch (err) {
          emit({ type: "$error", message: `tab_open failed: ${(err as Error).message}` });
        }
      })();
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
        process.env.DEEPSEEK_API_KEY = key;
        for (const tab of tabs.values()) {
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

    const tab = msg.tabId ? tabs.get(msg.tabId) : first;
    if (!tab) {
      emit({ type: "$error", message: `unknown tab: ${msg.tabId}` });
      return;
    }

    if (msg.cmd === "abort") {
      tab.aborter?.abort();
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
      try {
        parseMcpSpec(spec);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_add: ${(err as Error).message}` }, tab.id);
        return;
      }
      try {
        const cfg = readConfig();
        const list = cfg.mcp ?? [];
        if (!list.includes(spec)) {
          cfg.mcp = [...list, spec];
          writeConfig(cfg);
        }
        emitMcpSpecs(tab);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_add: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "mcp_specs_remove") {
      try {
        const cfg = readConfig();
        const list = cfg.mcp ?? [];
        if (list.includes(msg.spec)) {
          cfg.mcp = list.filter((s) => s !== msg.spec);
          writeConfig(cfg);
        }
        emitMcpSpecs(tab);
      } catch (err) {
        emit({ type: "$error", message: `mcp_specs_remove: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "skills_get") {
      emitSkills(tab);
      return;
    }
    if (msg.cmd === "session_list") {
      emitSessions(tab);
      return;
    }
    if (msg.cmd === "session_delete") {
      deleteSession(msg.name);
      emitSessions(tab);
      return;
    }
    if (msg.cmd === "session_load") {
      try {
        const records = loadSessionMessages(msg.name);
        const meta = loadSessionMeta(msg.name);
        tab.aborter?.abort();
        cancelPendingGates(tab);
        tab.currentSession = msg.name;
        if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
        emit(
          {
            type: "$session_loaded",
            name: msg.name,
            messages: buildLoadedMessages(records),
            carryover: {
              totalCostUsd: meta.totalCostUsd ?? 0,
              cacheHitTokens: meta.cacheHitTokens ?? 0,
              cacheMissTokens: meta.cacheMissTokens ?? 0,
            },
          },
          tab.id,
        );
      } catch (err) {
        emit({ type: "$error", message: `session_load failed: ${(err as Error).message}` }, tab.id);
      }
      return;
    }
    if (msg.cmd === "new_chat") {
      tab.aborter?.abort();
      cancelPendingGates(tab);
      tab.currentSession = mintSessionFor(tab.rootDir);
      if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
      emitSessions(tab);
      return;
    }
    if (msg.cmd === "settings_get") {
      emitSettings(tab);
      return;
    }
    if (msg.cmd === "settings_save") {
      try {
        if (msg.reasoningEffort !== undefined) {
          saveReasoningEffort(msg.reasoningEffort);
          tab.runtime?.loop.configure({ reasoningEffort: msg.reasoningEffort });
        }
        if (msg.editMode !== undefined) saveEditMode(msg.editMode);
        if (msg.budgetUsd !== undefined) {
          tab.budgetUsd = msg.budgetUsd ?? undefined;
          tab.runtime?.loop.setBudget(msg.budgetUsd);
        }
        if (msg.baseUrl !== undefined) saveBaseUrl(msg.baseUrl);
        if (msg.workspaceDir !== undefined) {
          void switchWorkspace(tab, msg.workspaceDir);
          return;
        }
        if (msg.editor !== undefined) saveEditor(msg.editor);
        if (msg.preset !== undefined) {
          tab.currentPreset = canonicalPresetName(msg.preset);
          const resolved = resolvePreset(tab.currentPreset);
          tab.currentModel = resolved.model;
          savePreset(tab.currentPreset);
          saveReasoningEffort(resolved.reasoningEffort);
          tab.system = codeSystemPrompt(tab.rootDir, {
            hasSemanticSearch: tab.toolset.semantic.enabled,
            modelId: tab.currentModel,
          });
          if (tab.runtime) tab.runtime = buildRuntimeFor(tab);
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
    if (msg.cmd === "mention_query") {
      const nonce = msg.nonce;
      const query = msg.query;
      const parsed = parseAtQuery(query);
      if (parsed.trailingSlash) {
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

  await new Promise<void>((resolve) => rl.on("close", resolve));
}
