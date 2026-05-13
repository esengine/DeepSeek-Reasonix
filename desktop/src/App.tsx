import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { relaunch } from "@tauri-apps/plugin-process";
import { type Update, check } from "@tauri-apps/plugin-updater";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { CommandPalette, Toast, buildCommands, useCommandPalette } from "./CommandPalette";
import { I } from "./icons";
import { getLang, setLang } from "./i18n";
import { WorkspaceProvider } from "./Markdown";
import type {
  CheckpointVerdict,
  ChoiceVerdict,
  ConfirmationChoice,
  IncomingEvent,
  McpSpecInfo,
  OutgoingCommand,
  PlanVerdict,
  RevisionVerdict,
  SettingsPatch,
  SkillInfo,
} from "./protocol";
import { Composer, type SlashCmd } from "./ui/composer";
import { ContextPanel } from "./ui/context-panel";
import { InterruptBar, useElapsed } from "./ui/live";
import { type PageId as SettingsPageId, SettingsModal } from "./ui/settings";
import { Sidebar } from "./ui/sidebar";
import { Splash, shouldShowSplash } from "./ui/splash";
import { StatusBar } from "./ui/statusbar";
import { WorkdirPop } from "./ui/workdir-pop";
import {
  ActivePlanTaskCard,
  AssistantMsg,
  CheckpointApprovalCard,
  ChoiceApprovalCard,
  ConfirmApprovalCard,
  PlanApprovalCard,
  PlanBanner,
  RevisionApprovalCard,
  TurnDivider,
  UserMsg,
} from "./ui/thread";

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

export type ChatMessage =
  | { kind: "user"; text: string; clientId: string; turn: number }
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
  summary?: string;
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

type MentionResults = { nonce: number; query: string; results: string[] };
type MentionPreviewState = {
  nonce: number;
  path: string;
  head: string;
  totalLines: number;
};

type State = {
  ready: boolean;
  needsSetup: boolean;
  busy: boolean;
  model?: string;
  currentSession?: string;
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
  mcpSpecs: McpSpecInfo[];
  mcpBridged: boolean;
  skills: SkillInfo[];
};

type DeltaBatchItem = {
  turn: number;
  channel: "content" | "reasoning";
  text: string;
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
    case "send_user": {
      const lastTurn = state.messages.reduce((max, m) => {
        if (m.kind === "user" || m.kind === "assistant") return Math.max(max, m.turn);
        return max;
      }, 0);
      return {
        ...state,
        busy: true,
        messages: [
          ...state.messages,
          { kind: "user", text: action.text, clientId: action.clientId, turn: lastTurn + 1 },
        ],
      };
    }
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
        currentSession: undefined,
        messages: [],
        pendingConfirms: [],
        pendingChoices: [],
        pendingPlans: [],
        pendingCheckpoints: [],
        pendingRevisions: [],
        activePlan: null,
        usage: zeroUsage(),
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

function zeroUsage(): UsageStats {
  return {
    totalCostUsd: 0,
    totalPromptTokens: 0,
    totalCompletionTokens: 0,
    cacheHitTokens: 0,
    cacheMissTokens: 0,
    lastCallCacheHit: null,
    lastCallCacheMiss: null,
  };
}

function appendTextSegment(
  segments: AssistantSegment[],
  kind: "text" | "reasoning",
  text: string,
): AssistantSegment[] {
  const last = segments[segments.length - 1];
  if (last && last.kind === kind) {
    return [...segments.slice(0, -1), { ...last, text: last.text + text }];
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
    case "$mcp_specs":
      return { ...state, mcpSpecs: ev.specs, mcpBridged: ev.bridged };
    case "$skills":
      return { ...state, skills: ev.items };
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
        usage: wsChanged ? zeroUsage() : state.usage,
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
      const sessionName = ev.name;
      const loaded: ChatMessage[] = ev.messages.map((m, i) => {
        if (m.kind === "user") {
          return { kind: "user", text: m.text, clientId: `c-loaded-${i}`, turn: i + 1 };
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
        currentSession: sessionName,
        messages: loaded,
        pendingConfirms: [],
        pendingChoices: [],
        pendingPlans: [],
        pendingCheckpoints: [],
        pendingRevisions: [],
        activePlan: null,
        usage: {
          ...zeroUsage(),
          totalCostUsd: ev.carryover.totalCostUsd,
          totalPromptTokens: ev.carryover.cacheHitTokens + ev.carryover.cacheMissTokens,
          cacheHitTokens: ev.carryover.cacheHitTokens,
          cacheMissTokens: ev.carryover.cacheMissTokens,
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
  theme: "dark" | "light";
  onToggleTheme: () => void;
  sideCollapsed: boolean;
  ctxCollapsed: boolean;
  onToggleSide: () => void;
  onToggleCtx: () => void;
  onToggleCurrency: () => void;
  tabsList: { id: string; workspaceDir?: string }[];
  activeTabId: string;
  setActiveTabId: (id: string) => void;
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
  theme,
  onToggleTheme,
  sideCollapsed,
  ctxCollapsed,
  onToggleSide,
  onToggleCtx,
  onToggleCurrency,
  tabsList,
  activeTabId,
  setActiveTabId,
}: TabRuntimeProps) {
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
    usage: zeroUsage(),
    sessions: [],
    settings: null,
    balance: null,
    mentionResults: null,
    mentionPreview: null,
    mcpSpecs: [],
    mcpBridged: false,
    skills: [],
  });
  const [draft, setDraft] = useState("");
  const [toast, setToast] = useState<string | null>(null);
  const [splashOn, setSplashOn] = useState<boolean>(() => shouldShowSplash());
  const [wdOpen, setWdOpen] = useState(false);
  const [wdAnchor, setWdAnchor] = useState<{ top: number; left: number } | undefined>(undefined);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const threadRef = useRef<HTMLDivElement>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsPage, setSettingsPage] = useState<SettingsPageId>("general");
  const openSettingsAt = useCallback((page: SettingsPageId = "general") => {
    setSettingsPage(page);
    setSettingsOpen(true);
  }, []);
  const palette = useCommandPalette();

  useEffect(() => {
    registerDispatch(tabId, dispatch);
    return () => registerDispatch(tabId, null);
  }, [tabId, registerDispatch]);

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
  const addMcpSpec = useCallback(
    (spec: string) => sendRpc({ cmd: "mcp_specs_add", spec }),
    [sendRpc],
  );
  const removeMcpSpec = useCallback(
    (spec: string) => sendRpc({ cmd: "mcp_specs_remove", spec }),
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

  useEffect(() => {
    if (!threadRef.current) return;
    threadRef.current.scrollTo({ top: threadRef.current.scrollHeight, behavior: "smooth" });
  }, [state.messages.length]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && (e.key === "l" || e.key === "L")) {
        e.preventDefault();
        composerRef.current?.focus();
      } else if (mod && (e.key === "n" || e.key === "N")) {
        e.preventDefault();
        newChat();
      } else if (mod && (e.key === "o" || e.key === "O")) {
        e.preventDefault();
        setWdAnchor(undefined);
        setWdOpen((v) => !v);
      } else if (mod && e.key === ",") {
        e.preventDefault();
        if (settingsOpen) setSettingsOpen(false);
        else openSettingsAt("general");
      } else if (e.key === "Escape" && state.busy) {
        const target = e.target as HTMLElement | null;
        if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") return;
        e.preventDefault();
        abort();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [state.busy, abort, newChat, settingsOpen, openSettingsAt]);

  const commands = buildCommands({
    newChat: () => {
      newChat();
      flashToast("已创建新会话");
    },
    clearChat: () => {
      dispatch({ t: "clear" });
      flashToast("已清空");
    },
    focusComposer: () => composerRef.current?.focus(),
    openSettings: () => openSettingsAt("general"),
    about: () => flashToast(`Reasonix · ${state.settings?.version ?? "dev"}`),
    abort,
    copyLast: () => {
      const last = [...state.messages].reverse().find((m) => m.kind === "assistant");
      if (!last || last.kind !== "assistant") return;
      const text = last.segments
        .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
        .map((s) => s.text)
        .join("\n\n")
        .trim();
      if (text) {
        void navigator.clipboard.writeText(text);
        flashToast("已复制");
      }
    },
    exportMarkdown: () => {
      const md = state.messages
        .map((m) => {
          if (m.kind === "user") return `### 你\n\n${m.text}`;
          if (m.kind === "assistant") {
            const body = m.segments
              .map((s) => {
                if (s.kind === "text") return s.text;
                if (s.kind === "reasoning") return `<details>\n<summary>Reasoning</summary>\n\n${s.text}\n\n</details>`;
                return "";
              })
              .filter(Boolean)
              .join("\n\n");
            return `### Reasonix\n\n${body}`;
          }
          if (m.kind === "error") return `### Error\n\n${m.message}`;
          return "";
        })
        .filter(Boolean)
        .join("\n\n---\n\n");
      if (md) {
        void navigator.clipboard.writeText(md);
        flashToast("已复制 Markdown");
      }
    },
    pickWorkspace,
    newTab: onNewTab,
    closeTab: onCloseTab,
    busy: state.busy,
    canCloseTab,
    hasMessages: state.messages.length > 0,
  });

  const slashCommands: SlashCmd[] = [
    { cmd: "/new", desc: "新建会话", run: () => newChat(), kb: "⌘N" },
    { cmd: "/clear", desc: "清空当前对话", run: () => dispatch({ t: "clear" }) },
    { cmd: "/abort", desc: "中断流式输出", run: () => abort(), kb: "esc" },
    {
      cmd: "/copy",
      desc: "复制最后一条回复",
      run: () => {
        const last = [...state.messages].reverse().find((m) => m.kind === "assistant");
        if (last?.kind === "assistant") {
          const text = last.segments
            .filter((s): s is { kind: "text"; text: string } => s.kind === "text")
            .map((s) => s.text)
            .join("\n\n");
          if (text) {
            void navigator.clipboard.writeText(text);
            flashToast("已复制");
          }
        }
      },
    },
    { cmd: "/model", desc: "切换模型", run: () => openSettingsAt("models") },
    { cmd: "/theme", desc: "切换深浅主题", run: onToggleTheme },
    {
      cmd: "/currency",
      desc: "切换货币显示 (CNY / USD)",
      run: onToggleCurrency,
    },
    {
      cmd: "/lang",
      desc: "切换界面语言 (中 / 英)",
      run: () => {
        const next = getLang() === "zh-CN" ? "en" : "zh-CN";
        setLang(next);
        flashToast(next === "zh-CN" ? "已切换到中文" : "switched to English");
      },
    },
    {
      cmd: "/export",
      desc: "复制本会话为 Markdown",
      run: () => exportConversation(),
    },
  ];

  const elapsed = useElapsed(state.busy);
  const workspaceLabel = state.settings?.workspaceDir
    ? state.settings.workspaceDir.split(/[\\/]/).pop() || "workspace"
    : "Reasonix";
  const session = (() => {
    const firstUser = state.messages.find((m) => m.kind === "user");
    if (firstUser && firstUser.kind === "user") {
      const cleaned = firstUser.text.replace(/\s+/g, " ").trim();
      if (cleaned) return cleaned.length > 60 ? `${cleaned.slice(0, 60)}…` : cleaned;
    }
    if (state.currentSession) {
      const s = state.sessions.find((x) => x.name === state.currentSession);
      if (s?.summary && s.summary.trim()) return s.summary.trim();
      const m = state.currentSession.match(/^desktop-(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})/);
      if (m) return `会话 ${m[2]}-${m[3]} ${m[4]}:${m[5]}`;
    }
    return state.messages.length === 0 ? `${workspaceLabel} · 新会话` : workspaceLabel;
  })();

  const exportConversation = useCallback(() => {
    const md = state.messages
      .map((m) => {
        if (m.kind === "user") return `### 你\n\n${m.text}`;
        if (m.kind === "assistant") {
          const body = m.segments
            .map((s) => {
              if (s.kind === "text") return s.text;
              if (s.kind === "reasoning")
                return `<details>\n<summary>Reasoning</summary>\n\n${s.text}\n\n</details>`;
              if (s.kind === "tool") {
                const arg = s.args ? `\n\n\`\`\`json\n${s.args}\n\`\`\`` : "";
                const res = s.result ? `\n\n\`\`\`\n${s.result}\n\`\`\`` : "";
                return `> **Tool · \`${s.name}\`**${arg}${res}`;
              }
              return "";
            })
            .filter(Boolean)
            .join("\n\n");
          return `### Reasonix\n\n${body}`;
        }
        if (m.kind === "error") return `### Error\n\n${m.message}`;
        return "";
      })
      .filter(Boolean)
      .join("\n\n---\n\n");
    if (md) {
      void navigator.clipboard.writeText(md);
      flashToast("已复制 Markdown");
    } else {
      flashToast("会话为空");
    }
  }, [state.messages, flashToast]);

  return (
    <WorkspaceProvider
      value={{ dir: state.settings?.workspaceDir, editor: state.settings?.editor }}
    >
      <div
        className="app"
        data-theme={theme}
        data-side-collapsed={sideCollapsed}
        data-ctx-collapsed={ctxCollapsed}
        style={{ display: active ? undefined : "none" }}
      >
        <TitleBar
          session={session}
          model={state.settings?.model}
          sideOn={!sideCollapsed}
          ctxOn={!ctxCollapsed}
          onToggleSide={onToggleSide}
          onToggleCtx={onToggleCtx}
          onOpenCommands={() => palette.setOpen(true)}
          onOpenSettings={() => openSettingsAt("general")}
          onExport={exportConversation}
          onClear={() => dispatch({ t: "clear" })}
          hasMessages={state.messages.length > 0}
        />

        <TabBar
          tabs={tabsList}
          activeId={activeTabId}
          setActive={setActiveTabId}
          onClose={(id) => {
            if (tabsList.length <= 1) return;
            invoke("rpc_send", {
              line: JSON.stringify({ cmd: "tab_close", tabId: id }),
            }).catch((err) => console.error("tab_close failed", err));
          }}
          onNew={onNewTab}
          singleTab={tabsList.length <= 1}
        />


        <Sidebar
          sessions={state.sessions}
          activeName={state.currentSession}
          onNewChat={newChat}
          onLoadSession={(name) => sendRpc({ cmd: "session_load", name })}
          onDeleteSession={(name) => sendRpc({ cmd: "session_delete", name })}
          onOpenSettings={() => openSettingsAt("general")}
          onOpenRules={() => openSettingsAt("rules")}
          onOpenCommands={() => palette.setOpen(true)}
        />

        <main className="main" style={{ position: "relative" }}>
          {state.needsSetup ? (
            <NeedsSetupView
              workspaceDir={state.settings?.workspaceDir}
              onPickWorkspace={pickWorkspace}
              onSubmit={(key) => sendRpc({ cmd: "setup_save_key", key })}
            />
          ) : (
            <>
              <MainHead
                session={session}
                model={state.settings?.model}
                workspaceDir={state.settings?.workspaceDir}
                busy={state.busy}
                hasMessages={state.messages.length > 0}
                onAbort={abort}
                onNewChat={newChat}
                onExport={exportConversation}
                onOpenWorkdir={(anchor) => {
                  setWdAnchor(anchor);
                  setWdOpen(true);
                }}
              />
              {state.settings?.editMode === "yolo" ? (
                <div className="mode-banner">
                  <span className="mb-pip" />
                  <I.warn size={13} />
                  <span className="mb-tag">YOLO</span>
                  <span className="mb-msg">
                    所有工具调用、shell 命令、文件编辑都会<b>自动批准</b>，不会再询问。
                  </span>
                  <span className="grow" />
                  <button
                    type="button"
                    className="mb-btn"
                    onClick={() => saveSettings({ editMode: "review" })}
                  >
                    切回 Review
                  </button>
                </div>
              ) : null}

              <div className="thread" ref={threadRef}>
                <div className="thread-inner">
                  {pendingUpdate ? (
                    <UpdateBanner
                      version={pendingUpdate.version}
                      currentVersion={pendingUpdate.currentVersion}
                      status={updateStatus}
                      onInstall={installUpdate}
                      onDismiss={dismissUpdate}
                    />
                  ) : null}

                  {state.activePlan ? (
                    <>
                      <PlanBanner
                        plan={state.activePlan}
                        onDismiss={state.busy ? undefined : () => dispatch({ t: "dismiss_plan" })}
                      />
                      <ActivePlanTaskCard plan={state.activePlan} />
                    </>
                  ) : null}

                  {state.messages.length === 0 ? (
                    <EmptyState
                      onPick={(t) => send(t)}
                      workspaceDir={state.settings?.workspaceDir}
                    />
                  ) : null}

                  {state.messages.map((m, i) => {
                    if (m.kind === "user") {
                      const dividerLabel = `turn ${m.turn}`;
                      const prev = state.messages[i - 1];
                      const needsDivider = !prev || prev.kind === "user";
                      return (
                        <div key={`u-${i}`}>
                          {needsDivider ? <TurnDivider label={dividerLabel} /> : null}
                          <UserMsg text={m.text} />
                        </div>
                      );
                    }
                    if (m.kind === "assistant") {
                      return (
                        <AssistantMsg
                          key={`a-${m.turn}`}
                          segments={m.segments}
                          pending={m.pending}
                          model={state.model}
                          onApproveConfirm={(id) =>
                            resolveConfirm(id, { type: "run_once" })
                          }
                          onRejectConfirm={(id) => resolveConfirm(id, { type: "deny" })}
                          onAlwaysAllowConfirm={(id, prefix) =>
                            resolveConfirm(id, { type: "always_allow", prefix })
                          }
                          pendingConfirms={state.pendingConfirms}
                        />
                      );
                    }
                    if (m.kind === "error") {
                      return (
                        <div key={`e-${i}`} className="warn-card" style={{ borderColor: "var(--tone-err)", background: "var(--danger-soft)" }}>
                          <span className="ico" style={{ color: "var(--tone-err)" }}>
                            <I.warning size={16} />
                          </span>
                          <div>
                            <div className="tt">错误</div>
                            <div className="ds">{m.message}</div>
                          </div>
                        </div>
                      );
                    }
                    return null;
                  })}

                  {/* Pending approvals */}
                  {state.pendingPlans.map((p) => (
                    <PlanApprovalCard
                      key={`pp-${p.id}`}
                      p={p}
                      onApprove={() => resolvePlan(p.id, { type: "approve" })}
                      onRefine={() => resolvePlan(p.id, { type: "refine" })}
                      onCancel={() => resolvePlan(p.id, { type: "cancel" })}
                    />
                  ))}
                  {state.pendingCheckpoints.map((c) => (
                    <CheckpointApprovalCard
                      key={`cp-${c.id}`}
                      c={c}
                      onContinue={() => resolveCheckpoint(c.id, { type: "continue" })}
                      onRevise={() => resolveCheckpoint(c.id, { type: "revise" })}
                      onStop={() => resolveCheckpoint(c.id, { type: "stop" })}
                    />
                  ))}
                  {state.pendingRevisions.map((r) => (
                    <RevisionApprovalCard
                      key={`rv-${r.id}`}
                      r={r}
                      onAccept={() => resolveRevision(r.id, { type: "accepted" })}
                      onReject={() => resolveRevision(r.id, { type: "rejected" })}
                    />
                  ))}
                  {state.pendingConfirms.map((c) => (
                    <ConfirmApprovalCard
                      key={`cc-${c.id}`}
                      c={c}
                      onAllow={() => resolveConfirm(c.id, { type: "run_once" })}
                      onAlwaysAllow={(prefix) =>
                        resolveConfirm(c.id, { type: "always_allow", prefix })
                      }
                      onDeny={() => resolveConfirm(c.id, { type: "deny" })}
                    />
                  ))}
                  {state.pendingChoices.map((c) => (
                    <ChoiceApprovalCard
                      key={`ch-${c.id}`}
                      c={c}
                      onPick={(optionId) => resolveChoice(c.id, { type: "pick", optionId })}
                      onCancel={() => resolveChoice(c.id, { type: "cancel" })}
                    />
                  ))}

                  {!state.ready ? (
                    <div
                      style={{
                        padding: 12,
                        color: "var(--muted)",
                        fontFamily: "IBM Plex Mono, monospace",
                        fontSize: 11,
                      }}
                    >
                      正在连接 reasonix 内核…
                    </div>
                  ) : null}
                </div>
              </div>

              <Composer
                draft={draft}
                setDraft={setDraft}
                onSend={() => send()}
                onAbort={abort}
                disabled={!state.ready}
                busy={state.busy}
                textareaRef={composerRef}
                preset={state.settings?.preset ?? "auto"}
                modelLabel={state.settings?.model ?? "deepseek-chat"}
                onPresetChange={(preset) => {
                  saveSettings({ preset });
                  flashToast(`已切换到 ${preset.toUpperCase()}`);
                }}
                editMode={state.settings?.editMode ?? "review"}
                onEditModeChange={(mode) => {
                  saveSettings({ editMode: mode });
                  flashToast(`模式: ${mode.toUpperCase()}`);
                }}
                workspaceDir={state.settings?.workspaceDir}
                slashCommands={slashCommands}
                onMentionQuery={queryMentions}
                onMentionPreview={previewMention}
                onMentionPicked={markMentionPicked}
                mentionResults={state.mentionResults}
              />

              {state.busy ? (
                <InterruptBar
                  visible={true}
                  elapsedMs={elapsed}
                  label="Reasoning"
                  onStop={abort}
                />
              ) : null}
            </>
          )}
        </main>

        <ContextPanel
          settings={state.settings}
          usage={state.usage}
          workspaceDir={state.settings?.workspaceDir}
          messages={state.messages}
        />

        <StatusBar
          settings={state.settings}
          balance={state.balance}
          usage={state.usage}
          busy={state.busy}
          ready={state.ready}
          currency={currency}
          theme={theme}
          onToggleTheme={onToggleTheme}
          onToggleCurrency={onToggleCurrency}
          onOpenSettings={() => openSettingsAt("general")}
        />

        <CommandPalette
          open={palette.open}
          onClose={() => palette.setOpen(false)}
          commands={commands}
        />

        <WorkdirPop
          open={wdOpen}
          onClose={() => setWdOpen(false)}
          recent={state.settings?.recentWorkspaces ?? []}
          current={state.settings?.workspaceDir}
          anchor={wdAnchor}
          onPick={(path) => saveSettings({ workspaceDir: path })}
          onBrowse={pickWorkspace}
        />

        {settingsOpen && state.settings ? (
          <SettingsModal
            settings={state.settings}
            balance={state.balance}
            usage={state.usage}
            currency={currency}
            initialPage={settingsPage}
            mcpSpecs={state.mcpSpecs}
            mcpBridged={state.mcpBridged}
            skills={state.skills}
            onClose={() => setSettingsOpen(false)}
            onSave={saveSettings}
            onSaveApiKey={saveApiKey}
            onPickWorkspace={pickWorkspace}
            onAddMcpSpec={addMcpSpec}
            onRemoveMcpSpec={removeMcpSpec}
          />
        ) : null}

        <Toast message={toast} />

        {splashOn ? <Splash onDone={() => setSplashOn(false)} /> : null}
      </div>
    </WorkspaceProvider>
  );
}

function TitleBar({
  session,
  model,
  sideOn,
  ctxOn,
  onToggleSide,
  onToggleCtx,
  onOpenCommands,
  onOpenSettings,
  onExport,
  onClear,
  hasMessages,
}: {
  session: string;
  model?: string;
  sideOn: boolean;
  ctxOn: boolean;
  onToggleSide: () => void;
  onToggleCtx: () => void;
  onOpenCommands: () => void;
  onOpenSettings: () => void;
  onExport: () => void;
  onClear: () => void;
  hasMessages: boolean;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const moreWrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!menuOpen) return;
    const onDown = (e: MouseEvent) => {
      if (moreWrapRef.current && !moreWrapRef.current.contains(e.target as Node)) setMenuOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [menuOpen]);
  return (
    <header className="titlebar">
      <div className="traffic">
        <span className="dot r" />
        <span className="dot y" />
        <span className="dot g" />
      </div>
      <div className="brand">
        <span className="mark" />
        <span>Reasonix</span>
      </div>
      <div className="crumbs">
        <span>{session}</span>
        <span className="sep">/</span>
        <span className="cur">{model ?? "—"}</span>
      </div>
      <span className="grow" />
      <div className="actions">
        <button
          type="button"
          className="iconbtn"
          data-on={sideOn}
          title="侧栏 (⌘B)"
          onClick={onToggleSide}
        >
          <I.panel_l size={14} />
        </button>
        <button
          type="button"
          className="iconbtn"
          data-on={ctxOn}
          title="上下文面板"
          onClick={onToggleCtx}
        >
          <I.panel_r size={14} />
        </button>
        <div ref={moreWrapRef} style={{ position: "relative" }}>
          <button
            type="button"
            className="iconbtn"
            title="更多"
            onClick={() => setMenuOpen((v) => !v)}
          >
            <I.more size={14} />
          </button>
        {menuOpen ? (
          <div
            className="popup"
            style={{
              top: "calc(100% + 6px)",
              right: 0,
              left: "auto",
              bottom: "auto",
              width: 220,
            }}
          >
            <div className="popup-list">
              <div
                className="popup-item"
                onClick={() => {
                  onOpenCommands();
                  setMenuOpen(false);
                }}
              >
                <span className="ico">
                  <I.search size={12} />
                </span>
                <div className="nm">
                  <span>命令面板</span>
                </div>
                <span className="kb">⌘K</span>
              </div>
              <div
                className="popup-item"
                onClick={() => {
                  if (hasMessages) onExport();
                  setMenuOpen(false);
                }}
                data-active={!hasMessages ? undefined : false}
                style={{ opacity: hasMessages ? 1 : 0.5 }}
              >
                <span className="ico">
                  <I.download size={12} />
                </span>
                <div className="nm">
                  <span>导出 Markdown</span>
                </div>
              </div>
              <div
                className="popup-item"
                onClick={() => {
                  onClear();
                  setMenuOpen(false);
                }}
              >
                <span className="ico">
                  <I.x size={12} />
                </span>
                <div className="nm">
                  <span>清空对话</span>
                </div>
              </div>
              <div
                className="popup-item"
                onClick={() => {
                  onOpenSettings();
                  setMenuOpen(false);
                }}
              >
                <span className="ico">
                  <I.cog size={12} />
                </span>
                <div className="nm">
                  <span>设置</span>
                </div>
                <span className="kb">⌘,</span>
              </div>
            </div>
          </div>
        ) : null}
        </div>
      </div>
    </header>
  );
}

function TabBar({
  tabs,
  activeId,
  setActive,
  onClose,
  onNew,
  singleTab,
}: {
  tabs: { id: string; workspaceDir?: string }[];
  activeId: string;
  setActive: (id: string) => void;
  onClose: (id: string) => void;
  onNew: () => void;
  singleTab?: boolean;
}) {
  return (
    <div className="tabbar">
      {tabs.map((t) => {
        const ws = t.workspaceDir ?? "";
        const label = ws.replace(/[\\/]$/, "").split(/[\\/]/).pop() || "workspace";
        return (
          <div
            key={t.id}
            className="tab"
            data-active={t.id === activeId}
            onClick={() => setActive(t.id)}
            title={ws || label}
          >
            <span className="dot" data-state="running" />
            <span className="label">{label}</span>
            {!singleTab ? (
              <span
                className="close"
                onClick={(e) => {
                  e.stopPropagation();
                  onClose(t.id);
                }}
              >
                <I.x size={11} />
              </span>
            ) : null}
          </div>
        );
      })}
      <div className="tab newtab" title="新建标签 ⌘T" onClick={onNew}>
        <I.plus size={11} />
        <span style={{ fontSize: 11, marginLeft: 4 }}>新标签</span>
      </div>
    </div>
  );
}

function MainHead({
  session,
  model,
  workspaceDir,
  busy,
  hasMessages,
  onAbort,
  onNewChat,
  onExport,
  onOpenWorkdir,
}: {
  session: string;
  model?: string;
  workspaceDir?: string;
  busy: boolean;
  hasMessages: boolean;
  onAbort: () => void;
  onNewChat: () => void;
  onExport: () => void;
  onOpenWorkdir: (anchor: { top: number; left: number }) => void;
}) {
  const wsLabel = workspaceDir ? workspaceDir.split(/[\\/]/).pop() || "workspace" : "未选择工作区";
  return (
    <div className="main-head">
      <div className="title-wrap">
        <h1>
          <span className="editable">{session}</span>
          {busy ? (
            <span className="pill" style={{ color: "var(--accent)" }}>
              <span className="dot" />
              <span className="shimmer">运行中</span>
            </span>
          ) : null}
        </h1>
        <div className="sub">
          <span
            className="ws-crumb"
            onClick={(e) => {
              const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
              onOpenWorkdir({ top: r.bottom + 6, left: r.left });
            }}
            style={{ cursor: "pointer" }}
            title={workspaceDir ?? "点击选择工作区"}
          >
            <I.folder size={10} /> {wsLabel}
          </span>
          {model ? (
            <span className="pill">
              <I.brain size={10} /> {model}
            </span>
          ) : null}
        </div>
      </div>
      <span className="grow" />
      <button
        type="button"
        className="h-btn"
        onClick={onExport}
        disabled={!hasMessages}
        title="复制对话为 Markdown"
      >
        <I.download size={12} /> 导出
      </button>
      <button type="button" className="h-btn" onClick={onNewChat}>
        <I.plus size={12} /> 新会话
      </button>
      {busy ? (
        <button type="button" className="h-btn primary" onClick={onAbort}>
          <I.stop size={12} /> 中断
        </button>
      ) : null}
    </div>
  );
}

function EmptyState({
  onPick,
  workspaceDir,
}: {
  onPick: (text: string) => void;
  workspaceDir?: string;
}) {
  const suggestions = [
    "帮我审查最近一次提交的代码改动",
    "把当前文件的 TS 报错都修了",
    "把 README 翻译成中英双语",
    "为这个仓库生成一份 CHANGELOG",
    "/help",
  ];
  const wsLabel = workspaceDir ? workspaceDir.split(/[\\/]/).pop() : null;
  return (
    <div
      style={{
        padding: "48px 16px 24px",
        textAlign: "center",
        color: "var(--muted)",
        fontFamily: "IBM Plex Sans, sans-serif",
      }}
    >
      <div
        style={{
          width: 56,
          height: 56,
          borderRadius: 12,
          margin: "0 auto 14px",
          background: "linear-gradient(135deg, var(--accent), var(--violet))",
          position: "relative",
        }}
      >
        <span
          style={{
            position: "absolute",
            inset: 8,
            borderRadius: 6,
            background: "var(--bg)",
          }}
        />
      </div>
      <div style={{ fontSize: 18, fontWeight: 600, color: "var(--fg)", marginBottom: 4 }}>
        欢迎使用 Reasonix
      </div>
      <div style={{ fontSize: 12, marginBottom: 18 }}>
        {wsLabel ? (
          <>
            当前工作区：<code style={{ fontFamily: "IBM Plex Mono, monospace" }}>{wsLabel}</code>
          </>
        ) : (
          "请先在顶部选择工作区"
        )}
      </div>
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 8,
          justifyContent: "center",
          maxWidth: 540,
          margin: "0 auto",
        }}
      >
        {suggestions.map((s) => (
          <button
            key={s}
            type="button"
            className="btn"
            style={{ fontSize: 11.5 }}
            onClick={() => onPick(s)}
          >
            {s}
          </button>
        ))}
      </div>
    </div>
  );
}

function NeedsSetupView({
  workspaceDir,
  onPickWorkspace,
  onSubmit,
}: {
  workspaceDir?: string;
  onPickWorkspace: () => void;
  onSubmit: (key: string) => void;
}) {
  const [key, setKey] = useState("");
  return (
    <div
      style={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        padding: 24,
        gap: 18,
      }}
    >
      <div style={{ fontSize: 18, fontWeight: 600 }}>欢迎使用 Reasonix</div>
      <div style={{ fontSize: 12.5, color: "var(--muted)", maxWidth: 400, textAlign: "center" }}>
        首次使用需要配置 DeepSeek API Key 与工作目录。Key 仅保存在本地。
      </div>
      <div
        style={{
          width: "min(420px, 100%)",
          display: "flex",
          flexDirection: "column",
          gap: 10,
        }}
      >
        <div className="setting-row" style={{ borderBottom: "none" }}>
          <div className="l">
            <div className="n">工作目录</div>
            <div className="h">{workspaceDir || "未选择"}</div>
          </div>
          <button type="button" className="btn" onClick={onPickWorkspace}>
            选择…
          </button>
        </div>
        <input
          className="field mono"
          type="password"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="sk-…"
          style={{ width: "100%" }}
        />
        <button
          type="button"
          className="btn primary"
          disabled={!key.trim()}
          onClick={() => onSubmit(key.trim())}
        >
          保存并开始
        </button>
      </div>
    </div>
  );
}

function UpdateBanner({
  version,
  currentVersion,
  status,
  onInstall,
  onDismiss,
}: {
  version: string;
  currentVersion: string;
  status: "idle" | "installing" | "error";
  onInstall: () => void;
  onDismiss: () => void;
}) {
  return (
    <div className="plan-banner" style={{ background: "var(--accent-soft)", borderColor: "var(--accent)" }}>
      <span className="ico">
        <I.download size={14} />
      </span>
      <div className="body">
        <div className="t">
          新版本可用 · {currentVersion} → {version}
        </div>
        <div className="s">{status === "installing" ? "正在安装…" : status === "error" ? "安装失败" : "点击安装并重启"}</div>
      </div>
      <div className="prog">
        <button type="button" onClick={onInstall} disabled={status === "installing"}>
          安装
        </button>
        <button type="button" onClick={onDismiss}>
          稍后
        </button>
      </div>
    </div>
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
  const [theme, setTheme] = useState<"dark" | "light">(() => {
    const v = localStorage.getItem("reasonix.theme");
    return v === "light" ? "light" : "dark";
  });
  const [sideCollapsed, setSideCollapsed] = useState(false);
  const [ctxCollapsed, setCtxCollapsed] = useState(false);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("reasonix.theme", theme);
  }, [theme]);

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
        // updater not configured
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
      } else if (mod && (e.key === "b" || e.key === "B")) {
        if (e.altKey) {
          e.preventDefault();
          setCtxCollapsed((v) => !v);
        } else {
          e.preventDefault();
          setSideCollapsed((v) => !v);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [openTab, closeTab, activeTabId, tabs]);

  const onToggleTheme = useCallback(() => {
    setTheme((t) => (t === "dark" ? "light" : "dark"));
  }, []);

  const onToggleCurrency = useCallback(() => {
    setCurrency((c) => {
      const next = c === "CNY" ? "USD" : "CNY";
      localStorage.setItem("reasonix.currency", next);
      window.dispatchEvent(new CustomEvent("reasonix:currency", { detail: next }));
      return next;
    });
  }, []);

  return (
    <>
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
          theme={theme}
          onToggleTheme={onToggleTheme}
          sideCollapsed={sideCollapsed}
          ctxCollapsed={ctxCollapsed}
          onToggleSide={() => setSideCollapsed((v) => !v)}
          onToggleCtx={() => setCtxCollapsed((v) => !v)}
          onToggleCurrency={onToggleCurrency}
          tabsList={tabs}
          activeTabId={activeTabId}
          setActiveTabId={setActiveTabId}
        />
      ))}
    </>
  );
}
