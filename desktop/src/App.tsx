import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { relaunch } from "@tauri-apps/plugin-process";
import { type Update, check } from "@tauri-apps/plugin-updater";
import { ArrowDown } from "lucide-react";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import { CommandPalette, Toast, buildCommands, useCommandPalette } from "./CommandPalette";
import { WorkspaceProvider } from "./Markdown";
import {
  ActivePlanRail,
  ApprovalCard,
  AssistantMessage,
  CheckpointCard,
  ChoiceCard,
  Composer,
  ContextMenu,
  type ContextMenuAction,
  EmptyState,
  ErrorBanner,
  Header,
  OnboardingScreen,
  PlanCard,
  RevisionCard,
  SettingsPanel,
  Sidebar,
  StatusLine,
  TabBar,
  ThinkingBar,
  UpdateBanner,
  UserBubble,
} from "./components";
import { t, useLang } from "./i18n";
import type {
  CheckpointVerdict,
  ChoiceVerdict,
  ConfirmationChoice,
  IncomingEvent,
  OutgoingCommand,
  PlanVerdict,
  RevisionVerdict,
  SettingsPatch,
} from "./protocol";

export type AssistantSegment =
  | { kind: "text"; text: string }
  | { kind: "reasoning"; text: string }
  | {
      kind: "tool";
      callId: string;
      name: string;
      args: string;
      startedAt: number;
      result?: string;
      ok?: boolean;
      durationMs?: number;
    };

type ChatMessage =
  | { kind: "user"; text: string; clientId: string }
  | {
      kind: "assistant";
      turn: number;
      segments: AssistantSegment[];
      pending: boolean;
    }
  | { kind: "status"; text: string }
  | { kind: "error"; message: string };

export type PendingConfirm = {
  id: number;
  kind: "run_command" | "run_background";
  command: string;
};

export type PendingChoice = {
  id: number;
  question: string;
  options: { id: string; title: string; summary?: string }[];
  allowCustom: boolean;
};

export type PendingPlan = {
  id: number;
  plan: string;
  summary?: string;
  steps?: PlanStep[];
};

export type PlanStep = {
  id: string;
  title: string;
  action: string;
  risk?: "low" | "med" | "high";
};

export type ActivePlan = {
  plan: string;
  summary?: string;
  steps: PlanStep[];
  completedStepIds: string[];
  /** Latest result for each completed step, keyed by stepId. */
  stepResults: Record<string, string>;
};

export type PendingCheckpoint = {
  id: number;
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
  completed: number;
  total: number;
};

export type PendingRevision = {
  id: number;
  reason: string;
  remainingSteps: PlanStep[];
  summary?: string;
};

export type UsageStats = {
  totalCostUsd: number;
  totalPromptTokens: number;
  totalCompletionTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  lastCallCacheHit: number | null;
  lastCallCacheMiss: number | null;
};

export type SessionInfo = {
  name: string;
  messageCount: number;
  mtime: string;
};

export type Settings = {
  reasoningEffort: "high" | "max";
  editMode: "review" | "auto" | "yolo";
  budgetUsd: number | null;
  baseUrl?: string;
  apiKeyPrefix?: string;
  workspaceDir: string;
  recentWorkspaces: string[];
  model: string;
  preset: "auto" | "flash" | "pro";
  editor?: string;
  version: string;
};

export type Balance = {
  currency: string;
  total: number;
  isAvailable: boolean;
};

type State = {
  ready: boolean;
  needsSetup: boolean;
  busy: boolean;
  model?: string;
  messages: ChatMessage[];
  pendingConfirms: PendingConfirm[];
  pendingChoices: PendingChoice[];
  pendingPlans: PendingPlan[];
  pendingCheckpoints: PendingCheckpoint[];
  pendingRevisions: PendingRevision[];
  activePlan: ActivePlan | null;
  usage: UsageStats;
  sessions: SessionInfo[];
  settings: Settings | null;
  balance: Balance | null;
  mentionResults: MentionResults | null;
  mentionPreview: MentionPreviewState | null;
};

type DeltaBatchItem = {
  turn: number;
  channel: "content" | "reasoning";
  text: string;
};

type MentionResults = { nonce: number; query: string; results: string[] };
type MentionPreviewState = {
  nonce: number;
  path: string;
  head: string;
  totalLines: number;
};

type Action =
  | { t: "send_user"; text: string; clientId: string }
  | { t: "incoming"; event: IncomingEvent }
  | { t: "batch_delta"; items: DeltaBatchItem[] }
  | { t: "rpc_exit"; code: number | null }
  | { t: "clear" }
  | { t: "resolve_confirm"; id: number }
  | { t: "resolve_choice"; id: number }
  | { t: "resolve_plan"; id: number; verdict: PlanVerdict }
  | { t: "resolve_checkpoint"; id: number; verdict: CheckpointVerdict }
  | { t: "resolve_revision"; id: number; verdict: RevisionVerdict }
  | { t: "dismiss_plan" }
  | { t: "mention_results"; results: MentionResults }
  | { t: "mention_preview"; preview: MentionPreviewState };

function reduce(state: State, action: Action): State {
  switch (action.t) {
    case "send_user":
      return {
        ...state,
        busy: true,
        messages: [
          ...state.messages,
          { kind: "user", text: action.text, clientId: action.clientId },
        ],
      };
    case "rpc_exit":
      return {
        ...state,
        ready: false,
        busy: false,
        messages: [
          ...state.messages,
          { kind: "error", message: `reasonix exited (code ${action.code ?? "?"})` },
        ],
      };
    case "incoming":
      return applyIncoming(state, action.event);
    case "batch_delta": {
      // Collapse same (turn, channel) adjacent items so we do one concat per cluster.
      const collapsed: DeltaBatchItem[] = [];
      for (const item of action.items) {
        const last = collapsed[collapsed.length - 1];
        if (last && last.turn === item.turn && last.channel === item.channel) {
          last.text += item.text;
        } else {
          collapsed.push({ ...item });
        }
      }
      return {
        ...state,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant") return m;
          const relevant = collapsed.filter((it) => it.turn === m.turn);
          if (relevant.length === 0) return m;
          let segments = m.segments;
          for (const it of relevant) {
            segments = appendTextSegment(
              segments,
              it.channel === "content" ? "text" : "reasoning",
              it.text,
            );
          }
          return { ...m, segments };
        }),
      };
    }
    case "clear":
      return {
        ...state,
        busy: false,
        messages: [],
        pendingConfirms: [],
        pendingChoices: [],
        pendingPlans: [],
        pendingCheckpoints: [],
        pendingRevisions: [],
        activePlan: null,
        usage: {
          totalCostUsd: 0,
          totalPromptTokens: 0,
          totalCompletionTokens: 0,
          cacheHitTokens: 0,
          cacheMissTokens: 0,
          lastCallCacheHit: null,
          lastCallCacheMiss: null,
        },
      };
    case "resolve_confirm":
      return {
        ...state,
        pendingConfirms: state.pendingConfirms.filter((c) => c.id !== action.id),
      };
    case "resolve_choice":
      return {
        ...state,
        pendingChoices: state.pendingChoices.filter((c) => c.id !== action.id),
      };
    case "resolve_plan": {
      const removed = state.pendingPlans.find((p) => p.id === action.id);
      let activePlan = state.activePlan;
      if (removed && action.verdict.type === "approve") {
        const pendingSteps = (removed as PendingPlan & { steps?: PlanStep[] }).steps;
        activePlan = {
          plan: removed.plan,
          summary: removed.summary,
          steps: pendingSteps ?? [],
          completedStepIds: [],
          stepResults: {},
        };
      }
      return {
        ...state,
        pendingPlans: state.pendingPlans.filter((p) => p.id !== action.id),
        activePlan,
      };
    }
    case "resolve_checkpoint":
      return {
        ...state,
        pendingCheckpoints: state.pendingCheckpoints.filter((c) => c.id !== action.id),
      };
    case "resolve_revision": {
      const removed = state.pendingRevisions.find((r) => r.id === action.id);
      let activePlan = state.activePlan;
      if (removed && action.verdict.type === "accepted" && activePlan) {
        const doneIds = new Set(activePlan.completedStepIds);
        const keptDone = activePlan.steps.filter((s) => doneIds.has(s.id));
        activePlan = {
          ...activePlan,
          steps: [...keptDone, ...removed.remainingSteps],
        };
      }
      return {
        ...state,
        pendingRevisions: state.pendingRevisions.filter((r) => r.id !== action.id),
        activePlan,
      };
    }
    case "dismiss_plan":
      return { ...state, activePlan: null };
    case "mention_results":
      return { ...state, mentionResults: action.results };
    case "mention_preview":
      return { ...state, mentionPreview: action.preview };
  }
}

function appendTextSegment(
  segments: AssistantSegment[],
  kind: "text" | "reasoning",
  text: string,
): AssistantSegment[] {
  const last = segments[segments.length - 1];
  if (last && last.kind === kind) {
    const updated = { ...last, text: last.text + text };
    return [...segments.slice(0, -1), updated];
  }
  return [...segments, { kind, text }];
}

function applyIncoming(state: State, ev: IncomingEvent): State {
  switch (ev.type) {
    case "$ready":
      return { ...state, ready: true, needsSetup: false };
    case "$needs_setup":
      return { ...state, needsSetup: true, ready: false };
    case "$turn_complete":
      return { ...state, busy: false };
    case "$confirm_required":
      return {
        ...state,
        pendingConfirms: [
          ...state.pendingConfirms,
          { id: ev.id, kind: ev.kind, command: ev.command },
        ],
      };
    case "$choice_required":
      return {
        ...state,
        pendingChoices: [
          ...state.pendingChoices,
          {
            id: ev.id,
            question: ev.question,
            options: ev.options,
            allowCustom: ev.allowCustom,
          },
        ],
      };
    case "$plan_required": {
      const steps = Array.isArray(ev.steps) ? (ev.steps as PlanStep[]) : undefined;
      return {
        ...state,
        pendingPlans: [
          ...state.pendingPlans,
          { id: ev.id, plan: ev.plan, summary: ev.summary, ...(steps ? { steps } : {}) },
        ],
      };
    }
    case "$checkpoint_required":
      return {
        ...state,
        pendingCheckpoints: [
          ...state.pendingCheckpoints,
          {
            id: ev.id,
            stepId: ev.stepId,
            title: ev.title,
            result: ev.result,
            notes: ev.notes,
            completed: ev.completed,
            total: ev.total,
          },
        ],
      };
    case "$revision_required":
      return {
        ...state,
        pendingRevisions: [
          ...state.pendingRevisions,
          {
            id: ev.id,
            reason: ev.reason,
            remainingSteps: ev.remainingSteps,
            summary: ev.summary,
          },
        ],
      };
    case "$step_completed": {
      if (!state.activePlan) return state;
      const stepIds = new Set(state.activePlan.completedStepIds);
      stepIds.add(ev.stepId);
      return {
        ...state,
        activePlan: {
          ...state.activePlan,
          completedStepIds: [...stepIds],
          stepResults: { ...state.activePlan.stepResults, [ev.stepId]: ev.result },
        },
      };
    }
    case "$plan_cleared":
      return {
        ...state,
        activePlan: null,
        pendingCheckpoints: [],
        pendingRevisions: [],
      };
    case "$sessions":
      return { ...state, sessions: ev.items };
    case "$balance":
      return {
        ...state,
        balance: {
          currency: ev.currency,
          total: ev.total,
          isAvailable: ev.isAvailable,
        },
      };
    case "$settings": {
      const prevWs = state.settings?.workspaceDir;
      const wsChanged = prevWs !== undefined && prevWs !== ev.workspaceDir;
      return {
        ...state,
        busy: wsChanged ? false : state.busy,
        messages: wsChanged ? [] : state.messages,
        pendingConfirms: wsChanged ? [] : state.pendingConfirms,
        pendingChoices: wsChanged ? [] : state.pendingChoices,
        pendingPlans: wsChanged ? [] : state.pendingPlans,
        pendingCheckpoints: wsChanged ? [] : state.pendingCheckpoints,
        pendingRevisions: wsChanged ? [] : state.pendingRevisions,
        activePlan: wsChanged ? null : state.activePlan,
        usage: wsChanged
          ? {
              totalCostUsd: 0,
              totalPromptTokens: 0,
              totalCompletionTokens: 0,
              cacheHitTokens: 0,
              cacheMissTokens: 0,
              lastCallCacheHit: null,
              lastCallCacheMiss: null,
            }
          : state.usage,
        settings: {
          reasoningEffort: ev.reasoningEffort,
          editMode: ev.editMode,
          budgetUsd: ev.budgetUsd,
          baseUrl: ev.baseUrl,
          apiKeyPrefix: ev.apiKeyPrefix,
          workspaceDir: ev.workspaceDir,
          recentWorkspaces: ev.recentWorkspaces,
          model: ev.model,
          preset: ev.preset,
          editor: ev.editor,
          version: ev.version,
        },
      };
    }
    case "$session_loaded": {
      // biome-ignore lint: same intent as below
      const loaded: ChatMessage[] = ev.messages.map((m, i) => {
        if (m.kind === "user") {
          return { kind: "user", text: m.text, clientId: `c-loaded-${i}` };
        }
        const segments: AssistantSegment[] = m.segments.map((s) => {
          if (s.kind === "tool") {
            return {
              kind: "tool",
              callId: s.callId,
              name: s.name,
              args: s.args,
              startedAt: 0,
              result: s.result,
              ok: s.ok,
              durationMs: 0,
            };
          }
          return s;
        });
        return { kind: "assistant", turn: m.turn, segments, pending: false };
      });
      return {
        ...state,
        busy: false,
        messages: loaded,
        pendingConfirms: [],
        pendingChoices: [],
        pendingPlans: [],
        pendingCheckpoints: [],
        pendingRevisions: [],
        activePlan: null,
        usage: {
          totalCostUsd: ev.carryover.totalCostUsd,
          totalPromptTokens: ev.carryover.cacheHitTokens + ev.carryover.cacheMissTokens,
          totalCompletionTokens: 0,
          cacheHitTokens: ev.carryover.cacheHitTokens,
          cacheMissTokens: ev.carryover.cacheMissTokens,
          lastCallCacheHit: null,
          lastCallCacheMiss: null,
        },
      };
    }
    case "$error":
    case "error":
      return {
        ...state,
        busy: false,
        messages: [...state.messages, { kind: "error", message: ev.message }],
      };
    case "model.turn.started":
      if (state.messages.some((m) => m.kind === "assistant" && m.turn === ev.turn)) {
        return { ...state, model: ev.model };
      }
      return {
        ...state,
        model: ev.model,
        messages: [
          ...state.messages,
          { kind: "assistant", turn: ev.turn, segments: [], pending: true },
        ],
      };
    case "model.delta":
      return {
        ...state,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant" || m.turn !== ev.turn) return m;
          if (ev.channel === "content") {
            return { ...m, segments: appendTextSegment(m.segments, "text", ev.text) };
          }
          if (ev.channel === "reasoning") {
            return { ...m, segments: appendTextSegment(m.segments, "reasoning", ev.text) };
          }
          return m;
        }),
      };
    case "model.final": {
      const u = ev.usage;
      const callHit = u?.prompt_cache_hit_tokens ?? 0;
      const callMiss = u?.prompt_cache_miss_tokens ?? 0;
      const hasCall = callHit > 0 || callMiss > 0;
      const usage: UsageStats = {
        totalCostUsd: state.usage.totalCostUsd + (ev.costUsd ?? 0),
        totalPromptTokens: state.usage.totalPromptTokens + (u?.prompt_tokens ?? 0),
        totalCompletionTokens: state.usage.totalCompletionTokens + (u?.completion_tokens ?? 0),
        cacheHitTokens: state.usage.cacheHitTokens + callHit,
        cacheMissTokens: state.usage.cacheMissTokens + callMiss,
        lastCallCacheHit: hasCall ? callHit : state.usage.lastCallCacheHit,
        lastCallCacheMiss: hasCall ? callMiss : state.usage.lastCallCacheMiss,
      };
      return {
        ...state,
        usage,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant" || m.turn !== ev.turn) return m;
          return { ...m, pending: false };
        }),
      };
    }
    case "tool.preparing":
      return {
        ...state,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant" || m.turn !== ev.turn) return m;
          if (m.segments.some((s) => s.kind === "tool" && s.callId === ev.callId)) return m;
          return {
            ...m,
            segments: [
              ...m.segments,
              {
                kind: "tool",
                callId: ev.callId,
                name: ev.name,
                args: "",
                startedAt: Date.now(),
              },
            ],
          };
        }),
      };
    case "tool.intent":
      return {
        ...state,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant" || m.turn !== ev.turn) return m;
          const idx = m.segments.findIndex((s) => s.kind === "tool" && s.callId === ev.callId);
          if (idx >= 0) {
            const segs = [...m.segments];
            const seg = segs[idx];
            if (seg?.kind === "tool") {
              segs[idx] = { ...seg, args: ev.args };
            }
            return { ...m, segments: segs };
          }
          return {
            ...m,
            segments: [
              ...m.segments,
              {
                kind: "tool",
                callId: ev.callId,
                name: ev.name,
                args: ev.args,
                startedAt: Date.now(),
              },
            ],
          };
        }),
      };
    case "tool.result":
      return {
        ...state,
        messages: state.messages.map((m) => {
          if (m.kind !== "assistant") return m;
          let mutated = false;
          const segs = m.segments.map((s) => {
            if (s.kind === "tool" && s.callId === ev.callId) {
              mutated = true;
              return {
                ...s,
                result: ev.output,
                ok: ev.ok,
                durationMs: Date.now() - s.startedAt,
              };
            }
            return s;
          });
          return mutated ? { ...m, segments: segs } : m;
        }),
      };
    case "status":
      return state;
    default:
      return state;
  }
}

function conversationToMarkdown(messages: ChatMessage[]): string {
  const parts: string[] = [];
  for (const m of messages) {
    if (m.kind === "user") {
      parts.push(`### 🧑 You\n\n${m.text}`);
      continue;
    }
    if (m.kind === "assistant") {
      const body: string[] = [];
      for (const s of m.segments) {
        if (s.kind === "text" && s.text) body.push(s.text);
        else if (s.kind === "reasoning" && s.text) {
          body.push(`<details>\n<summary>Reasoning</summary>\n\n${s.text}\n\n</details>`);
        } else if (s.kind === "tool") {
          const argLine = s.args ? `\n\`\`\`json\n${s.args}\n\`\`\`` : "";
          const resLine = s.result
            ? `\n\n<details>\n<summary>Result${s.ok === false ? " (error)" : ""}</summary>\n\n\`\`\`\n${s.result}\n\`\`\`\n\n</details>`
            : "";
          body.push(`> **Tool · \`${s.name}\`**${argLine}${resLine}`);
        }
      }
      parts.push(`### 🤖 Reasonix\n\n${body.join("\n\n")}`);
      continue;
    }
    if (m.kind === "error") {
      parts.push(`### ⚠ Error\n\n${m.message}`);
    }
  }
  return parts.join("\n\n---\n\n");
}

function tryParseArgs(raw: string): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw);
    return v && typeof v === "object" ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

function shortenPath(p: string, max = 44): string {
  if (p.length <= max) return p;
  const tail = p.slice(p.length - (max - 1));
  const cut = tail.search(/[\\/]/);
  return `…${cut > 0 ? tail.slice(cut) : tail}`;
}

function describeToolCall(name: string, args: string): string {
  const parsed = tryParseArgs(args);
  const path = parsed && typeof parsed.path === "string" ? shortenPath(parsed.path) : null;
  const pattern = parsed && typeof parsed.pattern === "string" ? parsed.pattern : null;
  const command =
    parsed && typeof parsed.command === "string"
      ? parsed.command.length > 50
        ? `${parsed.command.slice(0, 50)}…`
        : parsed.command
      : null;
  switch (name) {
    case "read_file":
      return path ? `Reading ${path}` : "Reading file";
    case "write_file":
    case "edit_file":
    case "multi_edit":
      return path ? `Editing ${path}` : "Editing file";
    case "list_directory":
    case "directory_tree":
      return path ? `Listing ${path}` : "Listing directory";
    case "search_content":
    case "search_files":
      return pattern ? `Searching ${pattern}` : "Searching";
    case "glob":
      return pattern ? `Globbing ${pattern}` : "Globbing";
    case "get_file_info":
      return path ? `Stat ${path}` : "Stat";
    case "create_directory":
      return path ? `mkdir ${path}` : "mkdir";
    case "delete_file":
    case "delete_directory":
      return path ? `Deleting ${path}` : "Deleting";
    case "move_file":
    case "copy_file":
      return path ? `${name === "move_file" ? "Moving" : "Copying"} ${path}` : name;
    case "run_command":
    case "run_background":
      return command ? `Running ${command}` : "Running command";
    default:
      return `Calling ${name}`;
  }
}

type TabAction = Action;
type TabDispatcher = (action: TabAction) => void;

interface TabRuntimeProps {
  tabId: string;
  active: boolean;
  currency: "CNY" | "USD";
  pendingUpdate: Update | null;
  updateStatus: "idle" | "installing" | "error";
  installUpdate: () => void;
  dismissUpdate: () => void;
  registerDispatch: (tabId: string, d: TabDispatcher | null) => void;
  onNewTab: () => void;
  onCloseTab: () => void;
  canCloseTab: boolean;
}

function TabRuntime({
  tabId,
  active,
  currency,
  pendingUpdate,
  updateStatus,
  installUpdate,
  dismissUpdate,
  registerDispatch,
  onNewTab,
  onCloseTab,
  canCloseTab,
}: TabRuntimeProps) {
  useLang();
  const [state, dispatch] = useReducer(reduce, {
    ready: false,
    needsSetup: false,
    busy: false,
    messages: [],
    pendingConfirms: [],
    pendingChoices: [],
    pendingPlans: [],
    pendingCheckpoints: [],
    pendingRevisions: [],
    activePlan: null,
    usage: {
      totalCostUsd: 0,
      totalPromptTokens: 0,
      totalCompletionTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      lastCallCacheHit: null,
      lastCallCacheMiss: null,
    },
    sessions: [],
    settings: null,
    balance: null,
    mentionResults: null,
    mentionPreview: null,
  });
  const [draft, setDraft] = useState("");
  const [toast, setToast] = useState<string | null>(null);
  const [ctxMenu, setCtxMenu] = useState<{
    x: number;
    y: number;
    actions: ContextMenuAction[];
  } | null>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const [atBottom, setAtBottom] = useState(true);
  const [settingsOpen, setSettingsOpen] = useState(false);
  useEffect(() => {
    registerDispatch(tabId, dispatch);
    return () => registerDispatch(tabId, null);
  }, [tabId, registerDispatch]);
  const mentionResults = state.mentionResults;
  const mentionPreview = state.mentionPreview;
  const palette = useCommandPalette();

  const sendRpc = useCallback(
    (cmd: OutgoingCommand) => {
      const payload = { tabId, ...cmd };
      invoke("rpc_send", { line: JSON.stringify(payload) }).catch((err) =>
        console.error(`${cmd.cmd} failed`, err),
      );
    },
    [tabId],
  );

  const queryMentions = useCallback(
    (query: string, nonce: number) => sendRpc({ cmd: "mention_query", query, nonce }),
    [sendRpc],
  );
  const previewMention = useCallback(
    (path: string, nonce: number) => sendRpc({ cmd: "mention_preview", path, nonce }),
    [sendRpc],
  );
  const markMentionPicked = useCallback(
    (path: string) => sendRpc({ cmd: "mention_picked", path }),
    [sendRpc],
  );
  const saveSettings = useCallback(
    (patch: SettingsPatch) => sendRpc({ cmd: "settings_save", ...patch }),
    [sendRpc],
  );
  const saveApiKey = useCallback(
    (key: string) => sendRpc({ cmd: "setup_save_key", key }),
    [sendRpc],
  );
  const newChat = useCallback(() => {
    sendRpc({ cmd: "new_chat" });
    dispatch({ t: "clear" });
  }, [sendRpc]);

  const pickWorkspace = useCallback(async () => {
    try {
      const picked = await openDialog({
        directory: true,
        multiple: false,
        title: "Pick workspace directory",
        defaultPath: state.settings?.workspaceDir,
      });
      if (typeof picked === "string" && picked.length > 0) {
        saveSettings({ workspaceDir: picked });
      }
    } catch (err) {
      console.error("pickWorkspace failed", err);
    }
  }, [saveSettings, state.settings?.workspaceDir]);

  const flashToast = useCallback((msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast(null), 1600);
  }, []);

  const messageActions = useCallback(
    (m: ChatMessage): ContextMenuAction[] => {
      const acts: ContextMenuAction[] = [];
      if (m.kind === "user") {
        acts.push({
          id: "copy",
          label: "Copy",
          icon: "⧉",
          run: () => {
            void navigator.clipboard.writeText(m.text);
            flashToast(t("toast.copied"));
          },
        });
        acts.push({
          id: "resend",
          label: "Resend",
          icon: "↺",
          run: () => setDraft(m.text),
        });
      } else if (m.kind === "assistant") {
        const text = m.segments
          .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
          .map((s) => s.text)
          .join("\n\n")
          .trim();
        if (text) {
          acts.push({
            id: "copy",
            label: "Copy reply",
            icon: "⧉",
            run: () => {
              void navigator.clipboard.writeText(text);
              flashToast(t("toast.copied"));
            },
          });
          acts.push({
            id: "quote",
            label: "Quote",
            icon: "❝",
            run: () => {
              const quoted = text
                .split("\n")
                .map((l) => `> ${l}`)
                .join("\n");
              setDraft((d) => (d ? `${d}\n\n${quoted}\n\n` : `${quoted}\n\n`));
              composerRef.current?.focus();
            },
          });
        }
      } else if (m.kind === "error") {
        acts.push({
          id: "copy",
          label: "Copy error",
          icon: "⧉",
          run: () => {
            void navigator.clipboard.writeText(m.message);
            flashToast(t("toast.copied"));
          },
        });
      }
      return acts;
    },
    [flashToast],
  );

  const send = useCallback(
    (override?: string) => {
      const text = (override ?? draft).trim();
      if (!text || !state.ready || state.busy) return;
      const clientId = `c-${Date.now()}`;
      dispatch({ t: "send_user", text, clientId });
      sendRpc({ cmd: "user_input", text });
      if (!override) setDraft("");
    },
    [draft, state.ready, state.busy, sendRpc],
  );

  const abort = useCallback(() => sendRpc({ cmd: "abort" }), [sendRpc]);

  const resolveConfirm = useCallback(
    (id: number, response: ConfirmationChoice) => {
      sendRpc({ cmd: "confirm_response", id, response });
      dispatch({ t: "resolve_confirm", id });
    },
    [sendRpc],
  );
  const resolveChoice = useCallback(
    (id: number, response: ChoiceVerdict) => {
      sendRpc({ cmd: "choice_response", id, response });
      dispatch({ t: "resolve_choice", id });
    },
    [sendRpc],
  );
  const resolvePlan = useCallback(
    (id: number, response: PlanVerdict) => {
      sendRpc({ cmd: "plan_response", id, response });
      dispatch({ t: "resolve_plan", id, verdict: response });
    },
    [sendRpc],
  );
  const resolveCheckpoint = useCallback(
    (id: number, response: CheckpointVerdict) => {
      sendRpc({ cmd: "checkpoint_response", id, response });
      dispatch({ t: "resolve_checkpoint", id, verdict: response });
    },
    [sendRpc],
  );
  const resolveRevision = useCallback(
    (id: number, response: RevisionVerdict) => {
      sendRpc({ cmd: "revision_response", id, response });
      dispatch({ t: "resolve_revision", id, verdict: response });
    },
    [sendRpc],
  );

  const copyLastReply = useCallback(() => {
    const last = [...state.messages].reverse().find((m) => m.kind === "assistant");
    if (!last || last.kind !== "assistant") return;
    const text = last.segments
      .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
      .map((s) => s.text)
      .join("\n\n")
      .trim();
    if (!text) return;
    void navigator.clipboard.writeText(text);
    flashToast(t("toast.copied"));
  }, [state.messages, flashToast]);

  const exportConversation = useCallback(() => {
    const md = conversationToMarkdown(state.messages);
    if (!md) return;
    void navigator.clipboard.writeText(md);
    flashToast(t("toast.copiedMd"));
  }, [state.messages, flashToast]);

  const versionLabel = state.settings?.version ?? "dev";
  const commands = buildCommands({
    newChat: () => {
      newChat();
      flashToast(t("toast.newSession"));
    },
    clearChat: () => {
      dispatch({ t: "clear" });
      flashToast(t("toast.cleared"));
    },
    focusComposer: () => {
      composerRef.current?.focus();
    },
    openSettings: () => setSettingsOpen(true),
    about: () => flashToast(t("toast.aboutLine", { version: versionLabel })),
    abort,
    copyLast: copyLastReply,
    exportMarkdown: exportConversation,
    pickWorkspace,
    newTab: onNewTab,
    closeTab: onCloseTab,
    busy: state.busy,
    canCloseTab,
    hasMessages: state.messages.length > 0,
  });

  const slashCommands = [
    {
      id: "new",
      label: t("palette.newChat"),
      hint: t("palette.newChatHint"),
      run: () => {
        newChat();
        flashToast(t("toast.newSession"));
      },
    },
    {
      id: "clear",
      label: t("palette.clearChat"),
      hint: t("palette.clearChatHint"),
      run: () => dispatch({ t: "clear" }),
    },
    {
      id: "abort",
      label: t("palette.abort"),
      hint: t("palette.abortHint"),
      run: () => abort(),
    },
    {
      id: "copy",
      label: t("palette.copyLast"),
      hint: t("palette.copyLastHint"),
      run: () => {
        const last = [...state.messages].reverse().find((m) => m.kind === "assistant");
        if (last && last.kind === "assistant") {
          const text = last.segments
            .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
            .map((s) => s.text)
            .join("\n\n")
            .trim();
          if (text) {
            void navigator.clipboard.writeText(text);
            flashToast(t("toast.copied"));
          }
        }
      },
    },
    {
      id: "export",
      label: t("palette.exportMd"),
      hint: t("palette.exportMdHint"),
      run: () => {
        const md = conversationToMarkdown(state.messages);
        if (md) {
          void navigator.clipboard.writeText(md);
          flashToast(t("toast.copiedMd"));
        }
      },
    },
  ];

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && (e.key === "l" || e.key === "L")) {
        e.preventDefault();
        composerRef.current?.focus();
      } else if (mod && (e.key === "n" || e.key === "N")) {
        e.preventDefault();
        newChat();
      } else if (e.key === "Escape" && state.busy) {
        const target = e.target as HTMLElement | null;
        if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") return;
        e.preventDefault();
        abort();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [state.busy, abort, newChat]);

  const hasMessages = state.messages.length > 0;
  const turnCount = state.messages.filter((m) => m.kind === "assistant").length;
  const [thinkStart, setThinkStart] = useState<number | null>(null);
  useEffect(() => {
    if (state.busy) {
      setThinkStart((prev) => prev ?? Date.now());
    } else {
      setThinkStart(null);
    }
  }, [state.busy]);

  // Derive what reasonix is currently doing from the latest assistant segment.
  const { activity, liveCompletionChars } = (() => {
    const latest = [...state.messages].reverse().find((m) => m.kind === "assistant");
    if (!latest || latest.kind !== "assistant") {
      return { activity: "Reasonix is thinking", liveCompletionChars: 0 };
    }
    const last = latest.segments[latest.segments.length - 1];
    const chars = latest.segments.reduce(
      (sum, s) => sum + (s.kind === "text" || s.kind === "reasoning" ? s.text.length : 0),
      0,
    );
    if (!last) return { activity: "Reasonix is thinking", liveCompletionChars: chars };
    if (last.kind === "tool") {
      if (last.result === undefined) {
        return {
          activity: describeToolCall(last.name, last.args),
          liveCompletionChars: chars,
        };
      }
      return { activity: "Continuing after tool", liveCompletionChars: chars };
    }
    if (last.kind === "reasoning") {
      return {
        activity: latest.pending ? "Reasoning" : "Continuing",
        liveCompletionChars: chars,
      };
    }
    return {
      activity: latest.pending ? "Writing response" : "Continuing",
      liveCompletionChars: chars,
    };
  })();

  // Completion tokens shown = settled past-turn total + rough estimate of
  // chars streamed in the active turn so far (≈4 chars / token, blends EN+CN).
  const liveCompletionTokens =
    state.usage.totalCompletionTokens + Math.ceil(liveCompletionChars / 4);

  return (
    <WorkspaceProvider
      value={{ dir: state.settings?.workspaceDir, editor: state.settings?.editor }}
    >
      <div className="app" style={{ display: active ? undefined : "none" }}>
        <Sidebar
          sessions={state.sessions}
          version={state.settings?.version}
          balance={state.balance}
          onNewChat={newChat}
          onOpenCommands={() => palette.setOpen(true)}
          onOpenSettings={() => setSettingsOpen(true)}
          onDeleteSession={(name) => sendRpc({ cmd: "session_delete", name })}
          onLoadSession={(name) => sendRpc({ cmd: "session_load", name })}
        />
        <div className="main">
          <Header
            model={state.model}
            preset={state.settings?.preset ?? "auto"}
            editMode={state.settings?.editMode ?? "review"}
            workspaceDir={state.settings?.workspaceDir}
            recentWorkspaces={state.settings?.recentWorkspaces ?? []}
            streaming={state.busy}
            turnCount={turnCount}
            usage={state.usage}
            onOpenCommands={() => palette.setOpen(true)}
            onPickPreset={(p) => saveSettings({ preset: p })}
            onPickEditMode={(m) => saveSettings({ editMode: m })}
            onPickWorkspace={pickWorkspace}
            onSwitchWorkspace={(dir) => saveSettings({ workspaceDir: dir })}
            currency={currency}
          />
          {active && pendingUpdate && (
            <UpdateBanner
              version={pendingUpdate.version}
              currentVersion={pendingUpdate.currentVersion}
              body={pendingUpdate.body}
              status={updateStatus}
              onInstall={installUpdate}
              onDismiss={dismissUpdate}
            />
          )}
          {state.activePlan && (
            <ActivePlanRail
              plan={state.activePlan.plan}
              summary={state.activePlan.summary}
              steps={state.activePlan.steps}
              completedStepIds={state.activePlan.completedStepIds}
              stepResults={state.activePlan.stepResults}
              onDismiss={state.busy ? undefined : () => dispatch({ t: "dismiss_plan" })}
            />
          )}
          {state.needsSetup ? (
            <OnboardingScreen
              workspaceDir={state.settings?.workspaceDir}
              onPickWorkspace={pickWorkspace}
              onSubmit={(key) => sendRpc({ cmd: "setup_save_key", key })}
            />
          ) : hasMessages ? (
            <div className="messages-wrap">
              <Virtuoso
                ref={virtuosoRef}
                className="messages-virt"
                data={state.messages}
                followOutput={(isAtBottom) => (isAtBottom ? "smooth" : false)}
                atBottomStateChange={setAtBottom}
                atBottomThreshold={200}
                increaseViewportBy={{ top: 200, bottom: 600 }}
                itemContent={(i, m) => {
                  const actions = messageActions(m);
                  const isLastError =
                    m.kind === "error" && i === state.messages.length - 1 && !state.busy;
                  const lastUserBefore = isLastError
                    ? (() => {
                        for (let j = i - 1; j >= 0; j--) {
                          const prev = state.messages[j];
                          if (prev?.kind === "user") return prev.text;
                        }
                        return null;
                      })()
                    : null;
                  const retry = lastUserBefore ? () => send(lastUserBefore) : undefined;
                  return (
                    <div
                      className="messages-row"
                      onContextMenu={
                        actions.length > 0
                          ? (e) => {
                              e.preventDefault();
                              setCtxMenu({ x: e.clientX, y: e.clientY, actions });
                            }
                          : undefined
                      }
                    >
                      <MessageRow message={m} onRetry={retry} />
                    </div>
                  );
                }}
                components={{
                  Footer: () => (
                    <div className="messages-footer">
                      {state.pendingConfirms.map((c) => (
                        <ApprovalCard
                          key={c.id}
                          kind={c.kind}
                          command={c.command}
                          onAllow={() => resolveConfirm(c.id, { type: "run_once" })}
                          onAlwaysAllow={(prefix) =>
                            resolveConfirm(c.id, { type: "always_allow", prefix })
                          }
                          onDeny={(reason) =>
                            resolveConfirm(c.id, { type: "deny", denyContext: reason })
                          }
                        />
                      ))}
                      {state.pendingChoices.map((c) => (
                        <ChoiceCard
                          key={c.id}
                          question={c.question}
                          options={c.options}
                          allowCustom={c.allowCustom}
                          onPick={(optionId) => resolveChoice(c.id, { type: "pick", optionId })}
                          onText={(text) => resolveChoice(c.id, { type: "text", text })}
                          onCancel={() => resolveChoice(c.id, { type: "cancel" })}
                        />
                      ))}
                      {state.pendingPlans.map((p) => (
                        <PlanCard
                          key={p.id}
                          plan={p.plan}
                          summary={p.summary}
                          onApprove={(feedback) => resolvePlan(p.id, { type: "approve", feedback })}
                          onRefine={(feedback) => resolvePlan(p.id, { type: "refine", feedback })}
                          onCancel={(feedback) => resolvePlan(p.id, { type: "cancel", feedback })}
                        />
                      ))}
                      {state.pendingCheckpoints.map((c) => (
                        <CheckpointCard
                          key={c.id}
                          stepId={c.stepId}
                          title={c.title}
                          result={c.result}
                          notes={c.notes}
                          completed={c.completed}
                          total={c.total}
                          onContinue={() => resolveCheckpoint(c.id, { type: "continue" })}
                          onRevise={(feedback) =>
                            resolveCheckpoint(c.id, { type: "revise", feedback })
                          }
                          onStop={() => resolveCheckpoint(c.id, { type: "stop" })}
                        />
                      ))}
                      {state.pendingRevisions.map((r) => (
                        <RevisionCard
                          key={r.id}
                          reason={r.reason}
                          remainingSteps={r.remainingSteps}
                          summary={r.summary}
                          onAccept={() => resolveRevision(r.id, { type: "accepted" })}
                          onReject={() => resolveRevision(r.id, { type: "rejected" })}
                          onCancel={() => resolveRevision(r.id, { type: "cancelled" })}
                        />
                      ))}
                      {!state.ready && <StatusLine text="connecting to reasonix" />}
                    </div>
                  ),
                }}
              />
              <button
                type="button"
                className={`scroll-fab ${atBottom ? "" : "show"}`}
                aria-label="scroll to bottom"
                onClick={() =>
                  virtuosoRef.current?.scrollToIndex({
                    index: state.messages.length - 1,
                    behavior: "smooth",
                  })
                }
              >
                <ArrowDown size={14} />
              </button>
            </div>
          ) : (
            <EmptyState onPick={(t) => send(t)} />
          )}
          {state.busy && thinkStart !== null && (
            <ThinkingBar
              startedAt={thinkStart}
              label={activity}
              promptTokens={state.usage.totalPromptTokens}
              completionTokens={liveCompletionTokens}
            />
          )}
          <Composer
            draft={draft}
            setDraft={setDraft}
            onSend={() => send()}
            onAbort={abort}
            onOpenCommands={() => palette.setOpen(true)}
            slashCommands={slashCommands}
            disabled={!state.ready}
            busy={state.busy}
            textareaRef={composerRef}
            onMentionQuery={queryMentions}
            onMentionPreview={previewMention}
            onMentionPicked={markMentionPicked}
            mentionResults={mentionResults}
            mentionPreview={mentionPreview}
          />
        </div>
        <CommandPalette
          open={palette.open}
          onClose={() => palette.setOpen(false)}
          commands={commands}
        />
        {ctxMenu && (
          <ContextMenu
            x={ctxMenu.x}
            y={ctxMenu.y}
            actions={ctxMenu.actions}
            onClose={() => setCtxMenu(null)}
          />
        )}
        {settingsOpen && state.settings && (
          <SettingsPanel
            settings={state.settings}
            onSave={saveSettings}
            onSaveApiKey={saveApiKey}
            onPickWorkspace={pickWorkspace}
            onClose={() => setSettingsOpen(false)}
          />
        )}
        <Toast message={toast} />
      </div>
    </WorkspaceProvider>
  );
}

type TabMeta = { id: string; workspaceDir?: string; busy?: boolean };

export function App() {
  const [tabs, setTabs] = useState<TabMeta[]>([]);
  const [activeTabId, setActiveTabId] = useState<string>("");
  const dispatchersRef = useRef<Map<string, TabDispatcher>>(new Map());
  const pendingEventsRef = useRef<Map<string, TabAction[]>>(new Map());
  const pendingDeltasRef = useRef<Map<string, DeltaBatchItem[]>>(new Map());
  const rafScheduledRef = useRef(false);
  const tabsRef = useRef<TabMeta[]>([]);
  useEffect(() => {
    tabsRef.current = tabs;
  }, [tabs]);

  const [pendingUpdate, setPendingUpdate] = useState<Update | null>(null);
  const [updateStatus, setUpdateStatus] = useState<"idle" | "installing" | "error">("idle");
  const [currency, setCurrency] = useState<"CNY" | "USD">(() => {
    const v = localStorage.getItem("reasonix.currency");
    return v === "USD" ? "USD" : "CNY";
  });
  useEffect(() => {
    const onCur = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (detail === "CNY" || detail === "USD") setCurrency(detail);
    };
    window.addEventListener("reasonix:currency", onCur);
    return () => window.removeEventListener("reasonix:currency", onCur);
  }, []);

  const deliverToTab = useCallback((tabId: string, action: TabAction) => {
    const dispatch = dispatchersRef.current.get(tabId);
    if (dispatch) {
      dispatch(action);
    } else {
      const buf = pendingEventsRef.current.get(tabId) ?? [];
      buf.push(action);
      pendingEventsRef.current.set(tabId, buf);
    }
  }, []);

  const registerDispatch = useCallback((tabId: string, d: TabDispatcher | null) => {
    if (d) {
      dispatchersRef.current.set(tabId, d);
      const buf = pendingEventsRef.current.get(tabId);
      if (buf && buf.length > 0) {
        for (const action of buf) d(action);
        pendingEventsRef.current.delete(tabId);
      }
    } else {
      dispatchersRef.current.delete(tabId);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const update = await check();
        if (!cancelled && update) setPendingUpdate(update);
      } catch {
        // updater not configured (no pubkey / endpoint), or network down — silent
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const installUpdate = useCallback(async () => {
    if (!pendingUpdate) return;
    setUpdateStatus("installing");
    try {
      await pendingUpdate.downloadAndInstall();
      await relaunch();
    } catch (err) {
      console.error("update failed", err);
      setUpdateStatus("error");
    }
  }, [pendingUpdate]);

  useEffect(() => {
    let cancelled = false;
    const cleanups: Array<() => void> = [];

    const flushDeltas = () => {
      rafScheduledRef.current = false;
      for (const [tabId, items] of pendingDeltasRef.current) {
        if (items.length === 0) continue;
        deliverToTab(tabId, { t: "batch_delta", items });
        pendingDeltasRef.current.set(tabId, []);
      }
    };
    const scheduleFlush = () => {
      if (rafScheduledRef.current || cancelled) return;
      rafScheduledRef.current = true;
      requestAnimationFrame(flushDeltas);
    };
    const flushTabDeltas = (tabId: string) => {
      const bucket = pendingDeltasRef.current.get(tabId);
      if (bucket && bucket.length > 0) {
        deliverToTab(tabId, { t: "batch_delta", items: bucket });
        pendingDeltasRef.current.set(tabId, []);
      }
    };

    const setup = async () => {
      const subs = await Promise.all([
        listen<{ data: string }>("rpc:event", (e) => {
          try {
            const ev = JSON.parse(e.payload.data) as IncomingEvent;
            const tabId = ev.tabId;

            if (ev.type === "$tab_opened" && tabId) {
              setTabs((prev) =>
                prev.some((t) => t.id === tabId)
                  ? prev
                  : [...prev, { id: tabId, workspaceDir: ev.workspaceDir }],
              );
              setActiveTabId(tabId);
              return;
            }
            if (ev.type === "$tab_closed" && tabId) {
              setTabs((prev) => prev.filter((t) => t.id !== tabId));
              setActiveTabId((prev) => {
                if (prev !== tabId) return prev;
                const remaining = tabsRef.current.filter((t) => t.id !== tabId);
                return remaining[0]?.id ?? "";
              });
              dispatchersRef.current.delete(tabId);
              pendingEventsRef.current.delete(tabId);
              pendingDeltasRef.current.delete(tabId);
              return;
            }

            if (ev.type === "model.delta" && tabId) {
              if (ev.channel === "content" || ev.channel === "reasoning") {
                const bucket = pendingDeltasRef.current.get(tabId) ?? [];
                bucket.push({ turn: ev.turn, channel: ev.channel, text: ev.text });
                pendingDeltasRef.current.set(tabId, bucket);
                scheduleFlush();
                return;
              }
            }

            if (ev.type === "$settings" && tabId) {
              setTabs((prev) =>
                prev.map((t) => (t.id === tabId ? { ...t, workspaceDir: ev.workspaceDir } : t)),
              );
              // fall through to also deliver to the tab reducer
            }

            const target = tabId;
            if (target) {
              flushTabDeltas(target);
              if (ev.type === "$mention_results") {
                deliverToTab(target, {
                  t: "mention_results",
                  results: { nonce: ev.nonce, query: ev.query, results: ev.results },
                });
                return;
              }
              if (ev.type === "$mention_preview") {
                deliverToTab(target, {
                  t: "mention_preview",
                  preview: {
                    nonce: ev.nonce,
                    path: ev.path,
                    head: ev.head,
                    totalLines: ev.totalLines,
                  },
                });
                return;
              }
              deliverToTab(target, { t: "incoming", event: ev });
            }
          } catch {
            console.error("bad rpc:event line", e.payload.data);
          }
        }),
        listen<{ data: string }>("rpc:stderr", (e) => {
          console.warn("[reasonix stderr]", e.payload.data);
        }),
        listen<{ code: number | null }>("rpc:exit", (e) => {
          for (const tabId of dispatchersRef.current.keys()) flushTabDeltas(tabId);
          for (const dispatch of dispatchersRef.current.values()) {
            dispatch({ t: "rpc_exit", code: e.payload.code });
          }
        }),
      ]);
      if (cancelled) {
        for (const u of subs) u();
        return;
      }
      cleanups.push(...subs);
      try {
        await invoke("rpc_spawn");
      } catch (err) {
        if (!cancelled) console.error("rpc_spawn failed", err);
      }
    };
    void setup();
    return () => {
      cancelled = true;
      for (const c of cleanups) c();
    };
  }, [deliverToTab]);

  const openTab = useCallback(() => {
    invoke("rpc_send", { line: JSON.stringify({ cmd: "tab_open" }) }).catch((err) =>
      console.error("tab_open failed", err),
    );
  }, []);

  const closeTab = useCallback(
    (id: string) => {
      if (tabs.length <= 1) return;
      invoke("rpc_send", { line: JSON.stringify({ cmd: "tab_close", tabId: id }) }).catch((err) =>
        console.error("tab_close failed", err),
      );
    },
    [tabs.length],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && (e.key === "t" || e.key === "T")) {
        e.preventDefault();
        openTab();
      } else if (mod && (e.key === "w" || e.key === "W") && activeTabId && tabs.length > 1) {
        e.preventDefault();
        closeTab(activeTabId);
      } else if (mod && e.key === "Tab") {
        if (tabs.length <= 1) return;
        e.preventDefault();
        const idx = tabs.findIndex((t) => t.id === activeTabId);
        const next = e.shiftKey ? (idx - 1 + tabs.length) % tabs.length : (idx + 1) % tabs.length;
        const target = tabs[next];
        if (target) setActiveTabId(target.id);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [openTab, closeTab, activeTabId, tabs]);

  return (
    <>
      {tabs.length > 1 && (
        <TabBar
          tabs={tabs}
          activeId={activeTabId}
          onActivate={setActiveTabId}
          onClose={closeTab}
          onNew={openTab}
        />
      )}
      {tabs.length === 1 && (
        <TabBar
          tabs={tabs}
          activeId={activeTabId}
          onActivate={setActiveTabId}
          onClose={closeTab}
          onNew={openTab}
          singleTab
        />
      )}
      {tabs.map((t) => (
        <TabRuntime
          key={t.id}
          tabId={t.id}
          active={t.id === activeTabId}
          currency={currency}
          pendingUpdate={pendingUpdate}
          updateStatus={updateStatus}
          installUpdate={installUpdate}
          dismissUpdate={() => setPendingUpdate(null)}
          registerDispatch={registerDispatch}
          onNewTab={openTab}
          onCloseTab={() => closeTab(t.id)}
          canCloseTab={tabs.length > 1}
        />
      ))}
    </>
  );
}

function MessageRow({
  message,
  onRetry,
}: {
  message: ChatMessage;
  onRetry?: () => void;
}) {
  switch (message.kind) {
    case "user":
      return <UserBubble text={message.text} />;
    case "assistant":
      return <AssistantMessage segments={message.segments} pending={message.pending} />;
    case "status":
      return <StatusLine text={message.text} />;
    case "error":
      return <ErrorBanner message={message.message} onRetry={onRetry} />;
  }
}
