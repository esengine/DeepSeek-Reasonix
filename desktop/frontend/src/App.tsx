import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Lightbulb,
  Download,
  SquarePen,
  Globe,
  FileText,
  FileJson,
  GitBranch,
  GitCommit,
  History,
  Settings as SettingsIcon,
  Pencil,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  Trash2,
} from "lucide-react";
import logoWordmark from "./assets/logo-wordmark.svg";
import { useToast } from "./lib/toast";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, t, useI18n, useT } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onProjectTreeChanged, onTurnDone, onEvent } from "./lib/bridge";
import { playSuccessChime } from "./lib/sound";
import { generativeMusic, isGenerativeMusicEnabled } from "./lib/generative-music";
import { Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { StatusBar } from "./components/StatusBar";
import { HistoryPanel } from "./components/HistoryPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import { UpdateBanner } from "./components/UpdateBanner";
import { ContextPanel } from "./components/ContextPanel";
import { DockTabBar } from "./components/DockTabBar";
import { DockContent } from "./components/DockContent";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { ProjectTree } from "./components/ProjectTree";
import { CopyButton } from "./components/CopyButton";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import type { ComposerInsertRequest, DockTab, Mode, SessionMeta, SettingsTab, TabMeta } from "./lib/types";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import {
  applyTheme,
  clearLegacyThemePreference,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
  themeForStyle,
  type Theme,
} from "./lib/theme";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { useWindowStatePersistence } from "./lib/windowState";

const SIDEBAR_COLLAPSED_KEY = "reasonix.sidebar.collapsed";
const DEFAULT_YOLO_KEY = "reasonix.defaultYolo";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_DEFAULT_RATIO = 0.175;
const SIDEBAR_MIN_WIDTH = 228;
const SIDEBAR_MAX_WIDTH = 420;
const SIDEBAR_COLLAPSED_WIDTH = 48;
const CHAT_MIN_WIDTH = 400;
const WORKSPACE_RESIZER_WIDTH = 8;

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}
const CONTEXT_PANEL_MIN_WIDTH = 340;
const RIGHT_DOCK_MIN_WIDTH = CONTEXT_PANEL_MIN_WIDTH;
const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 320;
const RIGHT_DOCK_TREE_DEFAULT_RATIO = 0.25;
const RIGHT_DOCK_TREE_MIN_WIDTH = 260;
const RIGHT_DOCK_TREE_MAX_WIDTH = 999999;
const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 640;
const RIGHT_DOCK_MAX_WIDTH = 999999;

const DEFAULT_DOCK_TABS: DockTab[] = [
  { id: "files", type: "files", title: "文件", icon: FileText, closable: false },
  { id: "changes", type: "changes", title: "改动", icon: GitBranch, closable: false },
  { id: "commit", type: "commit", title: "提交", icon: GitCommit, closable: false },
  { id: "memory", type: "memory", title: "记忆", icon: Lightbulb, closable: false },
];

type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampRightDockWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_MIN_WIDTH, Math.round(width)));
}

function clampRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

function viewportWidthFallback(): number {
  if (typeof window === "undefined") return 0;
  const width = Math.round(window.innerWidth || 0);
  return Number.isFinite(width) && width > 0 ? width : 0;
}

function defaultSidebarWidth(): number {
  const width = viewportWidthFallback();
  if (width <= 0) return SIDEBAR_DEFAULT_WIDTH;
  return clampSidebarWidth(width * SIDEBAR_DEFAULT_RATIO);
}

function defaultRightDockTreeWidth(): number {
  const width = viewportWidthFallback();
  if (width <= 0) return RIGHT_DOCK_TREE_DEFAULT_WIDTH;
  return clampRightDockTreeWidth(width * RIGHT_DOCK_TREE_DEFAULT_RATIO);
}

function resolveRightDockWidth(mainWidth: number, desiredDockWidth: number, minWidth: number): number {
  const budget = Math.max(0, Math.round(mainWidth) - CHAT_MIN_WIDTH - WORKSPACE_RESIZER_WIDTH);
  if (budget < minWidth) return 0;
  const desired = Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(minWidth, Math.round(desiredDockWidth)));
  return Math.min(budget, desired);
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidth", defaultSidebarWidth(), clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidth", width, clampSidebarWidth);
}

function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

function loadRightDockTreeWidth(): number {
  return loadLayoutSize("rightDockTreeWidth", defaultRightDockTreeWidth(), clampRightDockTreeWidth);
}

function saveRightDockTreeWidth(width: number): void {
  saveLayoutSize("rightDockTreeWidth", width, clampRightDockTreeWidth);
}

function loadRightDockPreviewWidth(): number {
  return loadLayoutSize("rightDockPreviewWidth", RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH, clampRightDockWidth);
}

function saveRightDockPreviewWidth(width: number): void {
  saveLayoutSize("rightDockPreviewWidth", width, clampRightDockWidth);
}

function tabWorkspaceTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  if (tab.scope === "project") return tab.workspaceName || tab.workspaceRoot || "Project";
  if (tab.scope === "global") return tab.workspaceName || "Global";
  return tab.workspaceName || tab.workspaceRoot || "Global";
}

function topicTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  const workspaceTitle = tabWorkspaceTitle(tab);
  const topic = tab.topicTitle || (tab.scope === "global" ? workspaceTitle : "Untitled");
  return topic === workspaceTitle ? workspaceTitle : `${workspaceTitle} / ${topic}`;
}

function topicScopeLabel(tab?: TabMeta): string {
  if (!tab) return t("scope.global");
  if (tab.scope === "global") return tab.workspaceName || t("scope.global");
  return t("scope.project", { name: tab.workspaceName || tab.workspaceRoot || "Project" });
}


function normalizeModeValue(mode?: string): Mode {
  return mode === "plan" || mode === "yolo" ? mode : "normal";
}

function loadDefaultYolo(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(DEFAULT_YOLO_KEY) === "1";
  } catch {
    return false;
  }
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function materializeLiveItems(items: Item[], live?: LiveStream): Item[] {
  if (!live) return items;
  return items.map((item) => {
    if (item.kind !== "assistant" || item.id !== live.id) return item;
    return { ...item, text: live.text, reasoning: live.reasoning, streaming: true };
  });
}

function fence(label: string, value: string): string {
  if (!value.trim()) return "";
  const fenceToken = value.includes("```") ? "````" : "```";
  return `${label}\n${fenceToken}\n${value.trim()}\n${fenceToken}`;
}

function sessionItemsToMarkdown(title: string, items: Item[], live?: LiveStream): string {
  const lines: string[] = [`# ${title.trim() || "Reasonix session"}`, ""];
  for (const item of materializeLiveItems(items, live)) {
    switch (item.kind) {
      case "user":
        lines.push("## User", "", item.text.trim(), "");
        break;
      case "assistant":
        lines.push("## Assistant");
        if (item.reasoning.trim()) {
          lines.push("", "### Reasoning", "", item.reasoning.trim());
        }
        if (item.text.trim()) {
          lines.push("", item.text.trim());
        }
        lines.push("");
        break;
      case "tool":
        lines.push(`### Tool: ${item.name}`);
        if (item.args.trim()) lines.push("", fence("Args", item.args));
        if (item.output?.trim()) lines.push("", fence("Output", item.output));
        if (item.error?.trim()) lines.push("", fence("Error", item.error));
        lines.push("");
        break;
      case "phase":
        lines.push(`### Phase`, "", item.text.trim(), "");
        break;
      case "notice":
        lines.push(`### ${item.level === "warn" ? "Warning" : "Notice"}`, "", item.text.trim(), "");
        break;
      case "compaction":
        lines.push("### Context Compaction", "");
        if (item.pending) {
          lines.push("Compaction pending.");
        } else {
          lines.push(`Messages: ${item.messages}`);
          if (item.trigger) lines.push(`Trigger: ${item.trigger}`);
          if (item.summary.trim()) lines.push("", item.summary.trim());
        }
        lines.push("");
        break;
    }
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n";
}

function sessionItemsToJson(title: string, items: Item[], live?: LiveStream): string {
  return JSON.stringify(
    {
      title,
      exportedAt: new Date().toISOString(),
      items: materializeLiveItems(items, live),
    },
    null,
    2,
  );
}

function safeFilename(name: string): string {
  const cleaned = name.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, " ").slice(0, 80);
  return cleaned || "reasonix-session";
}

function downloadTextFile(filename: string, text: string, mime: string): void {
  const blob = new Blob([text], { type: `${mime};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}


/** Global hotkey handler for shell-expand toggle (Ctrl/Cmd+B). */
function ShellHotkeys() {
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!shellExpand) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "b") {
        e.preventDefault();
        shellExpand.toggleLast();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [shellExpand]);
  return null;
}

/** Global hotkey handler for text-size shortcuts (Ctrl/Cmd + Plus/Minus/0). */
function TextSizeHotkeys() {
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      if (e.key !== "+" && e.key !== "=" && e.key !== "-" && e.key !== "0") return;

      e.preventDefault();
      if (e.key === "0") {
        applyTextSize(DEFAULT_TEXT_SIZE);
        return;
      }
      applyTextSize(nextTextSize(getTextSize(), e.key === "-" ? -1 : 1));
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);
  return null;
}

export default function App() {
  const {
    state,
    activeTabId,
    send,
    runShell,
    notice,
    cancel,
    approve,
    answerQuestion,
    setControllerMode,
    newSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    markTopicRead,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    setEffort,
    switchTab,
    openProjectTab,
    openGlobalTab,
    syncActiveTab,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [modesByTab, setModesByTab] = useState<Record<string, Mode>>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);

  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const { showToast } = useToast();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(true);
  const [rightDockTreeWidth, setRightDockTreeWidth] = useState(loadRightDockTreeWidth);
  const [rightDockPreviewWidth, setRightDockPreviewWidth] = useState(loadRightDockPreviewWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [dockTabs, setDockTabs] = useState<DockTab[]>(DEFAULT_DOCK_TABS);
  const [activeDockTabId, setActiveDockTabId] = useState("files");

  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const [topicExportOpen, setTopicExportOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);

  // ⌘K: open the command palette.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k" && !e.altKey && !e.shiftKey) {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  // Persist window geometry across launches.
  useWindowStatePersistence();

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const syncDesktopPreferences = async () => {
      const legacyLanguage = readLegacyLangPref();
      const legacyTheme = readLegacyThemePreference();
      if (legacyLanguage || legacyTheme.hasValue) {
        await app.MigrateDesktopPreferences(legacyLanguage, legacyTheme.theme, legacyTheme.style);
        clearLegacyLangPref();
        clearLegacyThemePreference();
      }
      const settings = await app.Settings();
      if (cancelled) return;
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyTheme(nextTheme, nextStyle, { persist: false });
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
    });
    return () => {
      cancelled = true;
    };
  }, [setLocalePref]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      setSettingsTarget("general");
    });
  }, []);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const layoutRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLElement>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const preferredWorkspacePanelWidth =
    workspacePreviewActive
      ? rightDockPreviewWidth
      : rightDockTreeWidth;
  const sidebarRenderWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : sidebarWidth;
  const measuredMainWidth = layoutWidth > 0 ? Math.max(0, layoutWidth - sidebarRenderWidth) : CHAT_MIN_WIDTH + WORKSPACE_RESIZER_WIDTH + preferredWorkspacePanelWidth;
  const workspacePanelMinWidth = workspacePreviewActive ? RIGHT_DOCK_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;

  const budget = Math.max(0, measuredMainWidth - CHAT_MIN_WIDTH - WORKSPACE_RESIZER_WIDTH);
  const workspacePanelFloating = workspacePanelOpen && !workspacePanelMaximized && budget < workspacePanelMinWidth;

  const resolvedWorkspacePanelWidth = workspacePanelOpen && !workspacePanelMaximized
    ? (workspacePanelFloating ? Math.min(measuredMainWidth, Math.max(workspacePanelMinWidth, preferredWorkspacePanelWidth)) : resolveRightDockWidth(measuredMainWidth, preferredWorkspacePanelWidth, workspacePanelMinWidth))
    : preferredWorkspacePanelWidth;

  const workspacePanelRenderable = workspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth > 0);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized && !workspacePanelFloating;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const mode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const setMode = useCallback(
    (next: Mode | ((prev: Mode) => Mode)) => {
      if (!activeTabId) return;
      setModesByTab((current) => {
        const prev = current[activeTabId] ?? "normal";
        const value = typeof next === "function" ? next(prev) : next;
        if (value === prev) return current;
        return { ...current, [activeTabId]: value };
      });
    },
    [activeTabId],
  );
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    const defaultYolo = loadDefaultYolo();
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        let mode = normalizeModeValue(tab.mode);
        // If defaultYolo is on and this is a brand-new tab (not in current), flip it to yolo
        if (defaultYolo && !(tab.id in current) && mode === "normal") {
          mode = "yolo";
        }
        next[tab.id] = mode;
        if (current[tab.id] !== mode) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [tabMetas]);

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  const syncModeToController = useCallback((m: Mode) => setControllerMode(m), [setControllerMode]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve every tool call). normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      setMode(m);
      void syncModeToController(m);
    },
    [setMode, syncModeToController],
  );
  // Shift+Tab cycles auto(normal) → plan → yolo → auto.
  const cycleMode = useCallback(() => {
    applyMode(mode === "normal" ? "plan" : mode === "plan" ? "yolo" : "normal");
  }, [mode, applyMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      await syncModeToController(mode);
    },
    [setModel, mode, syncModeToController],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and SetBypass was a
  // harmless no-op.
  useEffect(() => {
    if (state.meta?.ready !== true || mode === "normal") return;
    void syncModeToController(mode);
  }, [state.meta, mode, syncModeToController]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. It stays visible through the final
  // all-completed update, and can be dismissed by the user (the ✕). A dismissal
  // is keyed to that list's id, so a fresh accepted todo_write brings the panel
  // back.
  const todoEntry = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [state.items]);
  const todoItem = todoEntry?.item ?? null;
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const showTodos = shouldShowTodoPanel(todoItem?.id, dismissedTodo, todos);
  const [todoNow, setTodoNow] = useState(() => Date.now());
  const todoSeenRef = useRef<{ id: string; at: number } | null>(null);

  useEffect(() => {
    if (!todoItem) {
      todoSeenRef.current = null;
      return;
    }
    if (todoSeenRef.current?.id !== todoItem.id) {
      todoSeenRef.current = { id: todoItem.id, at: Date.now() };
      setTodoNow(Date.now());
    }
  }, [todoItem]);

  useEffect(() => {
    if (!showTodos) return;
    const id = window.setInterval(() => setTodoNow(Date.now()), 15000);
    return () => window.clearInterval(id);
  }, [showTodos]);

  // Play a success chime when any turn finishes (agent:turn-done event).
  useEffect(() => {
    const unsub = onTurnDone(() => {
      playSuccessChime();
    });
    return unsub;
  }, []);

  // Play generative ambient music while the agent is generating.
  // Start/stop the AudioContext engine based on running state.
  useEffect(() => {
    if (state.running && isGenerativeMusicEnabled()) {
      generativeMusic.start();
    } else {
      generativeMusic.stop();
    }
  }, [state.running]);

  // Per-token: play a note every time a text/reasoning chunk arrives.
  // playTokenNote() is a no-op when the engine isn't running.
  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind === "text" || e.kind === "reasoning" || e.kind === "tool_dispatch") {
        generativeMusic.playTokenNote();
      }
    });
    return unsub;
  }, []);

  const todoStale = useMemo(() => {
    if (!showTodos || !todoEntry) return false;
    const after = state.items.slice(todoEntry.index + 1);
    const completedToolsAfter = after.filter(
      (it) => it.kind === "tool" && it.name !== "todo_write" && !it.parentId && (it.status === "done" || it.status === "error"),
    ).length;
    const finalAssistantAfter = after.some((it) => it.kind === "assistant" && !it.streaming && it.text.trim() !== "");
    const readinessNoticeAfter = after.some(
      (it) => it.kind === "notice" && /final-answer readiness|todo_write|complete_step/i.test(it.text),
    );
    const staleByTime = state.running && todoSeenRef.current?.id === todoEntry.item.id && todoNow - todoSeenRef.current.at > 90_000;
    return completedToolsAfter >= 2 || finalAssistantAfter || readinessNoticeAfter || staleByTime;
  }, [showTodos, state.items, state.running, todoEntry, todoNow]);

  // useDeferredValue lets React prioritise Composer input (high-priority) over
  // Transcript re-renders (low-priority) during streaming. When a keystroke
  // and a transcript update collide, the keystroke is processed immediately
  // and the transcript re-render is deferred to idle time.
  const deferredItems = useDeferredValue(state.items);
  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
  const getSessionMarkdown = useCallback(
    () => sessionItemsToMarkdown(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );
  const getSessionJson = useCallback(
    () => sessionItemsToJson(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);

  const exportSession = useCallback(
    (format: "markdown" | "json") => {
      const base = safeFilename(sessionTitle);
      if (format === "json") downloadTextFile(`${base}.json`, getSessionJson(), "application/json");
      else downloadTextFile(`${base}.md`, getSessionMarkdown(), "text/markdown");
      setTopicExportOpen(false);
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    send(text);
  }, [pendingPlanRevision, send, state.running]);

  // handleSend intercepts the slash commands that need a desktop-native action
  // before they reach the backend: "/model <ref>" rebuilds on that model, and
  // "/memory" opens the Memory tab in the settings centre. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        runShell(cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        setSettingsTarget("memory");
        return;
      }
      const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
      if (theme) {
        const arg = theme[1]?.toLowerCase();
        if (!arg) {
          const cur = getTheme();
          notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
          return;
        }
        if (isThemeMode(arg)) {
          const next = arg;
          const style = getThemeStyle(next);
          await app.SetDesktopAppearance(next, style);
          applyTheme(next, style);
          notice(t("settings.themeChanged", { theme: next, style }));
          return;
        }
        if (isThemeStyle(arg)) {
          const next = themeForStyle(arg);
          await app.SetDesktopAppearance(next, arg);
          applyTheme(next, arg);
          notice(t("settings.themeChanged", { theme: next, style: arg }));
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      await syncModeToController(mode);
      send(trimmed, submitText.trim());
    },
    [switchModel, syncModeToController, mode, send, runShell, notice, t],
  );

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    setTabMetas(tabs);
    return tabs;
  }, []);

  useEffect(() => {
    void refreshTabMetas();
    const id = window.setInterval(() => void refreshTabMetas(), 2000);
    return () => window.clearInterval(id);
  }, [refreshTabMetas]);

  useEffect(() => {
    return onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas();
    });
  }, [refreshTabMetas]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) setNeedsOnboarding(needs);
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = () => setFooterHeight(Math.round(el.getBoundingClientRect().height));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const el = layoutRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = () => {
      const width = el.getBoundingClientRect().width;
      if (width && Number.isFinite(width)) setLayoutWidth(Math.round(width));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const startNewSession = useCallback(async () => {
    await newSession();
  }, [newSession]);

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((collapsed) => {
      const next = !collapsed;
      saveSidebarCollapsed(next);
      return next;
    });
  }, []);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    const next = clampSidebarWidth(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, []);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      event.preventDefault();
      setSidebarResizing(true);
      let nextWidth = sidebarWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampSidebarWidth(moveEvent.clientX);
        setSidebarWidth(nextWidth);
      };
      const onDone = () => {
        setSidebarWidth(nextWidth);
        saveSidebarWidth(nextWidth);
        setSidebarResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [sidebarCollapsed, sidebarWidth],
  );

  const resizeSidebarWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
      }
    },
    [setExpandedSidebarWidth, sidebarCollapsed, sidebarWidth],
  );

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      if (workspacePreviewActive) {
        const next = clampRightDockWidth(width);
        setRightDockPreviewWidth(next);
        saveRightDockPreviewWidth(next);
        return;
      }
      const next = clampRightDockTreeWidth(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [workspacePreviewActive],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      const next = clampRightDockWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = preferredWorkspacePanelWidth;
      let nextDockWidth = startDockWidth;
      // Write CSS variable directly during drag to avoid React re-render on
      // every pixel. Only state + localStorage are committed on pointerup.
      const root = document.documentElement;
      root.style.setProperty("--workspace-width", `${startDockWidth}px`);
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        root.style.setProperty("--workspace-width", `${Math.round(nextDockWidth)}px`);
      };
      const onDone = () => {
        setSavedWorkspacePanelWidth(nextDockWidth);
        setWorkspacePanelResizing(false);
        root.style.removeProperty("--workspace-width");
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [preferredWorkspacePanelWidth, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePreviewActive],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(preferredWorkspacePanelWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePreviewActive ? RIGHT_DOCK_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePreviewActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [preferredWorkspacePanelWidth, setSavedWorkspacePanelWidth, workspacePreviewActive],
  );

  const activateDockTab = useCallback(
    (id: string) => {
      const tab = dockTabs.find((t) => t.id === id);
      if (tab) {
        setActiveDockTabId(id);
        setWorkspacePanelOpen(true);
        setWorkspacePanelMaximized(false);
      }
    },
    [dockTabs],
  );

  const closeDockTab = useCallback(
    (id: string) => {
      setDockTabs((prev) => {
        const idx = prev.findIndex((t) => t.id === id);
        const next = prev.filter((t) => t.id !== id);
        if (next.length === 0) {
          setWorkspacePanelOpen(false);
        } else if (activeDockTabId === id) {
          const newIdx = Math.min(idx, next.length - 1);
          setActiveDockTabId(next[newIdx].id);
        }
        return next;
      });
    },
    [activeDockTabId],
  );

  const addBrowserTab = useCallback(() => {
    const id = `browser_${Date.now()}`;
    const newTab: DockTab = {
      id,
      type: "browser",
      title: "浏览器",
      icon: Globe,
      closable: true,
      metadata: { url: "about:blank", history: [], historyIndex: -1, isLoading: false },
    };
    setDockTabs((prev) => [...prev, newTab]);
    setActiveDockTabId(id);
    setWorkspacePanelOpen(true);
    setWorkspacePanelMaximized(false);
  }, []);

  const handleDockMetadataUpdate = useCallback(
    (id: string, meta: Record<string, unknown>) => {
      setDockTabs((prev) =>
        prev.map((t) =>
          t.id === id ? { ...t, metadata: { ...((t.metadata ?? {}) as Record<string, unknown>), ...meta } } : t,
        ),
      );
    },
    [],
  );

  const handleDockTitleUpdate = useCallback(
    (id: string, title: string) => {
      setDockTabs((prev) => prev.map((t) => (t.id === id ? { ...t, title } : t)));
    },
    [],
  );

  const openWorkspacePanel = useCallback(
    (mode: string) => {
      const existing = dockTabs.find((t) => t.id === mode || t.type === mode);
      if (existing) {
        setActiveDockTabId(existing.id);
        setWorkspacePanelOpen(true);
        setWorkspacePanelMaximized(false);
        return;
      }
      if (mode === "browser") {
        addBrowserTab();
      }
    },
    [dockTabs, addBrowserTab],
  );

  const closeWorkspacePanel = useCallback(() => {
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [workspacePanelOpen]);

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--chat-min-width": `${CHAT_MIN_WIDTH}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [sidebarWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      if (activeDockTabId) {
        setWorkspacePanelOpen(true);
        setWorkspacePanelMaximized(false);
      }
    } else {
      closeWorkspacePanel();
    }
  }, [activeDockTabId, closeWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);




  const handleMessageAction = useCallback(async (turn: number, scope: string) => {
    await rewind(turn, scope);
    if (scope === "fork") {
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      return;
    }
    if (scope === "code" || scope === "both") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
    }
  }, [refreshTabMetas, rewind]);

  const handleOpenTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    if (scope === "global") {
      await openGlobalTab(topicId);
    } else {
      await openProjectTab(workspaceRoot, topicId);
    }
    if (topicId) await markTopicRead(topicId);
    await refreshTabMetas();
    setDockRefreshKey((v) => v + 1);
  }, [markTopicRead, openGlobalTab, openProjectTab, refreshTabMetas, setDockRefreshKey]);

  // ⌘G: jump to the next unread topic that is not currently running
  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== "g") return;
      event.preventDefault();

      const findNextUnread = async () => {
        try {
          const tree = await app.ListProjectTree();
          const nodes = asArray(tree);

          // Flatten: collect topic / global_topic nodes in tree order
          const topics: { scope: string; root: string; topicId: string; running?: boolean; hasUnread?: boolean }[] = [];
          for (const node of nodes) {
            const children = asArray(node.children);
            for (const child of children) {
              if (child.kind === "topic" || child.kind === "global_topic") {
                topics.push({
                  scope: child.kind === "global_topic" ? "global" : "project",
                  root: child.root ?? "",
                  topicId: child.topicId ?? "",
                  running: child.running,
                  hasUnread: child.hasUnread,
                });
              }
            }
          }

          if (topics.length === 0) return;

          // Start looking after the current topic (or from the beginning)
          const currentId = activeTab?.topicId;
          const startIndex = currentId ? topics.findIndex((t) => t.topicId === currentId) + 1 : 0;

          // Priority 1: unread + not running (stopped generating but unread)
          for (let i = 0; i < topics.length; i++) {
            const idx = (startIndex + i) % topics.length;
            const topic = topics[idx];
            if (topic.hasUnread && !topic.running) {
              await handleOpenTopic(topic.scope, topic.root, topic.topicId);
              return;
            }
          }

          // Priority 2: running (currently generating)
          for (let i = 0; i < topics.length; i++) {
            const idx = (startIndex + i) % topics.length;
            const topic = topics[idx];
            if (topic.running) {
              await handleOpenTopic(topic.scope, topic.root, topic.topicId);
              return;
            }
          }
        } catch {
          /* bridge unavailable */
        }
      };

      void findNextUnread();
    };

    window.addEventListener("keydown", onKeyDown, { capture: true });
    return () => window.removeEventListener("keydown", onKeyDown, { capture: true });
  }, [activeTab, handleOpenTopic]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [listSessions]);
  const openAllHistory = useCallback(async () => {
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [listSessions]);
  const openTrash = useCallback(async () => {
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [listTrashedSessions]);
  const closeHistory = useCallback(() => setHistView(null), []);

  const handleTabChange = useCallback(async (tabId: string) => {
    await switchTab(tabId);
    setDockRefreshKey((v) => v + 1);
  }, [switchTab, setDockRefreshKey]);
  const handleNewTab = useCallback(async () => {
    await newSession();
  }, [newSession]);

  // ── Command palette (⌘K) item builder ───────────────────────────────
  const buildPaletteItems = useCallback((): PaletteItem[] => {
    const items: PaletteItem[] = [];

    for (const tab of tabMetas) {
      items.push({
        id: `tab:${tab.id}`,
        title: tab.topicTitle || "Untitled",
        hint: tab.scope === "global" ? "Global" : tab.workspaceName || tab.workspaceRoot,
        group: t("sidebar.conversations"),
        keywords: [tab.label],
        run: () => void handleTabChange(tab.id),
      });
    }

    items.push(
      {
        id: "action:new-session",
        title: t("topbar.newSession"),
        group: "Actions",
        keywords: ["new", "chat", "session"],
        run: () => void handleNewTab(),
      },
      {
        id: "action:history",
        title: t("sidebar.allHistory"),
        group: "Actions",
        keywords: ["history", "sessions", "past"],
        run: () => void openAllHistory(),
      },
      {
        id: "action:settings",
        title: t("topbar.settings"),
        group: "Actions",
        keywords: ["preferences", "config", "options"],
        run: () => setSettingsTarget("general"),
      },
      {
        id: "action:trash",
        title: t("sidebar.trash"),
        group: "Actions",
        keywords: ["deleted", "bin"],
        run: () => void openTrash(),
      },
    );

    return items;
  }, [tabMetas, handleTabChange, handleNewTab, openAllHistory, openTrash, t]);
  const onResumeSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running) return;
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      try {
        let targetTab: TabMeta;
        if (scope === "project" && session.workspaceRoot && session.topicId) {
          targetTab = await openProjectTab(session.workspaceRoot, session.topicId);
        } else if (scope === "global" && session.topicId) {
          targetTab = await openGlobalTab(session.topicId);
        } else {
          throw new Error(scope === "global" && !session.topicId
            ? t("history.failedOpenSession")
            : (session.topicId ? "Missing workspaceRoot" : t("history.failedOpenSession")));
        }
        setHistView(null);
        await resumeSession(session.path, targetTab.id);
        setTabRevealSignal((signal) => signal + 1);
      } catch (err: any) {
        setHistView(null);
        if (scope === "project" && session.workspaceRoot) {
          const name = workspaceDisplayName(session.workspaceRoot);
          showToast(t("history.failedOpenProject", { name, path: session.workspaceRoot }));
        } else {
          showToast(err?.message || String(err));
        }
      }
    },
    [openGlobalTab, openProjectTab, refreshTabMetas, state.running, resumeSession, t, showToast],
  );
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      if (state.running) return;
      await deleteSession(path);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, deleteSession, listSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      if (state.running) return;
      await renameSession(path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, renameSession, listSessions],
  );
  const onRestoreTrashedSession = useCallback(
    async (path: string) => {
      await restoreSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [restoreSession, listTrashedSessions],
  );
  const onPurgeTrashedSession = useCallback(
    async (path: string) => {
      await purgeTrashedSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeAllTrashedSessions = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        await purgeTrashedSession(path);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) {
      setProjectRevision((value) => value + 1);
      await refreshTabMetas();
    }
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshTabMetas]);

  const removeWorkspace = useCallback(async (path: string) => {
    await app.RemoveWorkspace(path);
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
  }, [refreshTabMetas]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas();
    if (activeTabId && !tabs.some((tab) => tab.id === activeTabId)) {
      await syncActiveTab(true);
    }
  }, [activeTabId, refreshTabMetas, syncActiveTab]);

  const renameTopic = useCallback(async (topicId: string, title: string) => {
    const nextTitle = title.trim();
    if (!topicId || !nextTitle) return;
    await app.RenameTopic(topicId, nextTitle);
    await refreshProjectsAndTabs();
  }, [refreshProjectsAndTabs]);

  const startActiveTopicRename = useCallback(() => {
    if (!activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.topicId, activeTab?.topicTitle]);

  const cancelActiveTopicRename = useCallback(() => {
    topicRenameSkipCommitRef.current = true;
    topicRenameCommitHandledRef.current = true;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, []);

  const commitActiveTopicRename = useCallback(async () => {
    if (topicRenameSkipCommitRef.current) {
      topicRenameSkipCommitRef.current = false;
      topicRenameCommitHandledRef.current = false;
      setRenamingTopicId(null);
      return;
    }
    if (topicRenameCommitHandledRef.current) return;
    topicRenameCommitHandledRef.current = true;
    const topicId = renamingTopicId;
    setRenamingTopicId(null);
    if (!topicId) return;
    const nextTitle = topicTitleDraft.trim();
    if (!nextTitle) return;
    try {
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const workspacePanelResetWidth = workspacePreviewActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : defaultRightDockTreeWidth();
  const workspacePanelMaxWidth = workspacePreviewActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;


  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
    <div className={`app app--${desktopPlatform}`}>
      <div
        ref={layoutRef}
        className={[
          "layout",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelFloating ? "layout--workspace-floating" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >


        <aside className={`sidebar${sidebarCollapsed ? " sidebar--collapsed" : ""}`} aria-label={t("sidebar.navigation")}>
          <div className="sidebar__header">
            <div className="sidebar__header-left">
              <Tooltip label={t("topbar.newSession")} fill>
                <button
                  className="sidebar__new-icon"
                  onClick={() => {
                    if (state.running) cancel();
                    void startNewSession();
                  }}
                  aria-label={t("topbar.newSession")}
                >
                  <SquarePen size={15} />
                </button>
              </Tooltip>
              <button
                className="sidebar__collapse-btn"
                type="button"
                onClick={toggleSidebar}
                aria-label={sidebarToggleTitle}
              >
                {sidebarCollapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
              </button>
            </div>
          </div>

          <section className="sidebar__section sidebar__section--projects">
            <ProjectTree
              activeScope={activeTab?.scope}
              activeWorkspaceRoot={activeTab?.workspaceRoot}
              activeTopicId={activeTab?.topicId}
              onOpenTopic={handleOpenTopic}
              onOpenProjectHistory={openProjectHistory}
              onTopicsChanged={refreshProjectsAndTabs}
              onRenameTopic={renameTopic}
              refreshSignal={projectRevision}
              onAddProject={async () => {
                await switchFolder();
              }}
            />
          </section>

          <nav className="sidebar__nav">
            <Tooltip label={t("sidebar.allHistory")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => void openAllHistory()}
              >
                <History size={15} />
                <span>{t("sidebar.allHistory")}</span>
              </button>
            </Tooltip>
            <Tooltip label={t("sidebar.trash")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => void openTrash()}
              >
                <Trash2 size={15} />
                <span>{t("sidebar.trash")}</span>
              </button>
            </Tooltip>
            <Tooltip label={t("topbar.settings")} fill side="right" disabled={sidebarNavTooltipDisabled}>
              <button
                className="sidebar__navitem"
                onClick={() => setSettingsTarget("general")}
              >
                <SettingsIcon size={15} />
                <span>{t("topbar.settings")}</span>
              </button>
            </Tooltip>
          </nav>

        </aside>
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={SIDEBAR_MIN_WIDTH}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarWidth}
          onPointerDown={startSidebarResize}
          onKeyDown={resizeSidebarWithKeyboard}
          onDoubleClick={() => setExpandedSidebarWidth(defaultSidebarWidth())}
        />

        <section className="chat-pane">

          <header className="topicbar">
            <div className="topicbar__identity">
              <div className="topicbar__title-row">
                {topicbarEditing ? (
                  <div className="topicbar__title-edit">
                    <input
                      autoFocus
                      className="topicbar__title-input"
                      value={topicTitleDraft}
                      onChange={(event) => setTopicTitleDraft(event.target.value)}
                      onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          void commitActiveTopicRename();
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          cancelActiveTopicRename();
                        }
                      }}
                      onBlur={() => void commitActiveTopicRename()}
                    />
                  </div>
                ) : (
                  <h1>{activeTab?.topicTitle || "Untitled"}</h1>
                )}
                <Tooltip label={t("topicBar.renameSession")}>
                  <button
                    className="topicbar__icon-btn"
                    type="button"
                    disabled={!activeTab?.topicId || topicbarEditing}
                    onClick={startActiveTopicRename}
                    aria-label={t("topicBar.renameSession")}
                  >
                    <Pencil size={14} />
                  </button>
                </Tooltip>
              </div>
            </div>
            <div className="topicbar__spacer" />
            <div className="topicbar__actions">
              <CopyButton
                getText={getSessionMarkdown}
                label={t("topicBar.copyAll")}
                showLabel={false}
                className="topicbar__action-btn topicbar__action-btn--icon"
              />
              <div className={`topicbar__export${topicExportOpen ? " topicbar__export--open" : ""}`}>
                <Tooltip label={t("topicBar.export")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon"
                    type="button"
                    disabled={!sessionHasContent}
                    aria-label={t("topicBar.export")}
                    aria-haspopup="menu"
                    aria-expanded={topicExportOpen}
                    onClick={() => setTopicExportOpen((open) => !open)}
                  >
                    <Download size={14} />
                  </button>
                </Tooltip>
                {topicExportOpen && (
                  <div className="topicbar__export-menu" role="menu">
                    <button type="button" role="menuitem" onClick={() => exportSession("markdown")}>
                      <FileText size={13} />
                      <span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => exportSession("json")}>
                      <FileJson size={13} />
                      <span>{t("topicBar.exportJson")}</span>
                    </button>
                  </div>
                )}
              </div>
              {!workspacePanelMaximized && (workspacePanelRenderable ? (
                <Tooltip label={t("rightDock.collapse")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon"
                    type="button"
                    onClick={closeWorkspacePanel}
                    aria-label={t("rightDock.collapse")}
                  >
                    <PanelRightClose size={15} />
                  </button>
                </Tooltip>
              ) : (
                <Tooltip label={t("rightDock.expand")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon"
                    type="button"
                    onClick={() => openWorkspacePanel("files")}
                    aria-label={t("rightDock.expand")}
                  >
                    <PanelRightOpen size={15} />
                  </button>
                </Tooltip>
              ))}
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner />

          <div className="chat-pane__body">
            <div className="chat-area">
              <main className="main">
                {state.meta?.ready === false && !state.meta?.startupErr ? (
                  <div className="loading-screen">
                    <div className="loading-screen__spinner" />
                    <span className="loading-screen__text">{t("common.loading")}</span>
                  </div>
                ) : (
                  <Transcript
                    items={deferredItems}
                    live={state.live}
                    footerHeight={footerHeight}
                    onPrompt={send}
                    onRewind={handleMessageAction}
                    checkpoints={state.checkpoints}
                    actionPending={state.messageAction != null}
                    rewindDisabled={state.running || state.messageAction != null || state.approval != null || state.ask != null}
                  />
                )}
              </main>

              <footer className="footer" ref={footerRef}>
                {showTodos && <TodoPanel todos={todos} stale={todoStale} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
                {state.approval && (
                  <ApprovalModal
                    approval={state.approval}
                    onAnswer={(allow, session, persist) => {
                      // Approving an exit_plan_mode plan leaves plan mode; sync the
                      // tab-local indicator and persisted safe mode immediately.
                      if (state.approval!.tool === "exit_plan_mode" && allow) applyMode("normal");
                      approve(state.approval!.id, allow, session, persist);
                    }}
                    onRevisePlan={(text) => {
                      setPendingPlanRevision(text);
                      approve(state.approval!.id, false, false, false);
                    }}
                    onExitPlan={() => {
                      applyMode("normal");
                      approve(state.approval!.id, false, false, false);
                    }}
                  />
                )}
                {state.ask && (
                  <AskCard
                    ask={state.ask}
                    onAnswer={answerQuestion}
                    onDismiss={() => answerQuestion(state.ask!.id, [])}
                  />
                )}
                <Composer
                  running={state.running}
                  mode={mode}
                  cwd={state.meta?.cwd}
                  modelLabel={state.meta?.label ?? t("status.connecting")}
                  tabId={activeTabId}
                  effort={state.effort}
                  onSend={handleSend}
                  onCancel={cancel}
                  onCycleMode={cycleMode}
                  onSwitchModel={switchModel}
                  onSetEffort={setEffort}
                  onPickFolder={switchFolder}
                  onRemoveWorkspace={removeWorkspace}
                  insertRequest={composerInsertRequest}
                  disabled={state.meta?.ready === false || state.messageAction != null || state.approval != null || state.ask != null}
                  decisionPending={state.messageAction != null || state.approval != null || state.ask != null}
                  ready={state.meta?.ready === true}
                  workspaceRefreshSignal={projectRevision}
                />
                <StatusBar
                  context={state.context}
                  usage={state.usage}
                  balance={state.balance}
                  jobs={state.jobs}
                  running={state.running}
                  mode={mode}
                  cost={state.sessionCost}
                  currency={state.sessionCurrency}
                  turnTokens={state.turnTokens}
                  turnStartAt={state.turnStartAt}
                />
              </footer>
            </div>

          </div>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className="workbench-dock"
            aria-label={t("rightDock.workbench")}
          >
            <DockTabBar
              tabs={dockTabs}
              activeId={activeDockTabId}
              onSelect={activateDockTab}
              onClose={closeDockTab}
              onAdd={addBrowserTab}
              rightExtra={null}
            />
            <div className="workbench-dock__body">
              {activeDockTabId === "context" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionCost={state.sessionCost}
                  sessionCurrency={state.sessionCurrency}
                  scopeLabel={topicScopeLabel(activeTab)}
                  refreshKey={dockRefreshKey}
                />
              ) : (
                dockTabs.map((tab) =>
                  tab.id === activeDockTabId ? (
                    <DockContent
                      key={tab.id}
                      tab={tab}
                      open={workspacePanelRenderable}
                      cwd={state.meta?.cwd}
                      maximized={workspacePanelMaximized}
                      panelWidth={workspacePanelRenderWidth}
                      onClose={() => setWorkspacePanel(false)}
                      onToggleMaximized={() => setWorkspacePanelMaximized((value) => !value)}
                      onPreviewModeChange={setWorkspacePreviewActive}
                      onAddToChat={addWorkspaceTextToComposer}
                      onRequestPanelWidth={ensureWorkspacePanelWidth}
                      refreshKey={dockRefreshKey}
                      onMetadataUpdate={handleDockMetadataUpdate}
                      onTitleUpdate={handleDockTitleUpdate}
                    />
                  ) : null,
                )
              )}
            </div>
          </aside>
        )}
      </div>

      {histView !== null && (
        <HistoryPanel
          kind={histView.kind}
          sessions={histView.sessions}
          running={state.running}
          onResume={onResumeSession}
          onPreview={previewSession}
          onDelete={onDeleteSession}
          onRename={onRenameSession}
          onRestore={onRestoreTrashedSession}
          onPurge={onPurgeTrashedSession}
          onPurgeAll={onPurgeAllTrashedSessions}
          onClose={closeHistory}
        />
      )}

      {settingsTarget !== null && (
        <SettingsPanel
          initialTab={settingsTarget}
          onClose={() => setSettingsTarget(null)}
          onChanged={() => void refreshMeta()}
        />
      )}

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        items={buildPaletteItems()}
        placeholder={t("topbar.newSession") + "..."}
        emptyText={t("sidebar.conversations")}
      />
    </div>
    </ShellExpandProvider>
  );
}
