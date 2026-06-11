import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Activity,
  Command,
  Download,
  SquarePen,
  FileDown,
  FileImage,
  FileText,
  FileJson,
  GitBranch,
  History,
  Settings as SettingsIcon,
  Pencil,
  Trash2,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, useI18n, useT } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onProjectTreeChanged } from "./lib/bridge";
import { Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { ClearContextCard } from "./components/ClearContextCard";
import { StatusBar } from "./components/StatusBar";
import { HistoryPanel } from "./components/HistoryPanel";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { SettingsPanel } from "./components/SettingsPanel";
import { UpdateBanner } from "./components/UpdateBanner";
import { ContextPanel } from "./components/ContextPanel";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { AppChrome } from "./components/AppChrome";
import { ProjectTree } from "./components/ProjectTree";
import { CopyButton } from "./components/CopyButton";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import {
  modeHasAutoApproveTools,
  modeHasPlan,
  modeFromAxes,
  normalizeMode,
  normalizeToolApprovalMode,
  type CollaborationMode,
  type ComposerInsertRequest,
  type Mode,
  type ProjectNode,
  type SessionMeta,
  type SettingsTab,
  type SettingsView,
  type TabMeta,
  type ToolApprovalMode,
} from "./lib/types";
import {
  controllerCollaborationMode,
  displayedCollaborationMode,
  keepGoalDraftMode,
  metaSyncedCollaborationMode,
  tabListCollaborationMode,
} from "./lib/goalDraftMode";
import {
  restorableToolApprovalMode,
  toggleYoloToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import { hydrateDisplayMode } from "./lib/displayMode";
import { blobToBase64, renderSessionImageBlob, renderSessionPdfBlob } from "./lib/sessionExport";
import {
  applyTheme,
  clearLegacyThemePreference,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
  type Theme,
} from "./lib/theme";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { useWindowStatePersistence } from "./lib/windowState";
import { availableWorkspacePanelWidth, resolveWorkspacePanelWidth, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import logoWordmark from "./assets/logo-wordmark.svg";

const SIDEBAR_COLLAPSED_KEY = "reasonix.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_MIN_WIDTH = 264;
const SIDEBAR_MAX_WIDTH = 300;
const SIDEBAR_VIEWPORT_RATIO = 0.18;
const CHAT_MIN_WIDTH = 400;
const CHAT_COMFORT_MIN_WIDTH = 560;
const WORKSPACE_RESIZER_WIDTH = 8;

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}
const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 300;
const RIGHT_DOCK_TREE_MIN_WIDTH = 300;
const RIGHT_DOCK_TREE_MAX_WIDTH = 560;
const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 660;
const RIGHT_DOCK_PREVIEW_MIN_WIDTH = 420;
const RIGHT_DOCK_MIN_RENDER_WIDTH = 280;
const RIGHT_DOCK_MAX_WIDTH = 860;

type RightDockMode = "context" | "files" | "changed";
type WorkspaceRevealRequest = { id: number; path: string };
type WorkspaceFileListRequest = { id: number; paths: string[] };
type WorkspaceChangeListEntry = { key: string; path: string; meta: string; time: string; detail: string };
type WorkspaceChangeListRequest = { id: number; changes: WorkspaceChangeListEntry[] };
const SHOW_CONTEXT_DOCK = true;
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };

function activeTopicTurnsFromTree(tree: ProjectNode[], tab?: TabMeta): number | undefined {
  if (!tab?.topicId) return undefined;
  const targetScope = tab.scope === "global" ? "global" : "project";
  const walk = (nodes: ProjectNode[]): number | undefined => {
    for (const node of nodes) {
      if (!node) continue;
      if (node.kind === "topic" || node.kind === "global_topic") {
        const scope = node.kind === "global_topic" ? "global" : "project";
        if (
          scope === targetScope &&
          node.topicId === tab.topicId &&
          (scope === "global" || node.root === tab.workspaceRoot)
        ) {
          return node.turns;
        }
      }
      const found = walk(asArray(node.children));
      if (found !== undefined) return found;
    }
    return undefined;
  };
  return walk(tree);
}

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampRightDockPreviewWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_PREVIEW_MIN_WIDTH, Math.round(width)));
}

function clampRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

function defaultSidebarWidth(): number {
  if (typeof window !== "undefined") {
    return clampSidebarWidth(window.innerWidth * SIDEBAR_VIEWPORT_RATIO);
  }
  return SIDEBAR_DEFAULT_WIDTH;
}

function defaultRightDockTreeWidth(): number {
  return RIGHT_DOCK_TREE_DEFAULT_WIDTH;
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
  return loadLayoutSize("sidebarWidthGraphite", defaultSidebarWidth(), clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidthGraphite", width, clampSidebarWidth);
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
  return loadLayoutSize("rightDockPreviewWidth", RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH, clampRightDockPreviewWidth);
}

function saveRightDockPreviewWidth(width: number): void {
  saveLayoutSize("rightDockPreviewWidth", width, clampRightDockPreviewWidth);
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

function topicDisplayTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  return tab.topicTitle || (tab.scope === "global" ? tabWorkspaceTitle(tab) : "Untitled");
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function workspaceDisplayName(path?: string): string {
  if (!path) return "";
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
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
    steer,
    notice,
    cancel,
    approve,
    answerQuestion,
    setControllerMode,
    setCollaborationMode: setControllerCollaborationMode,
    setToolApprovalMode: setControllerToolApprovalMode,
    setGoal: setControllerGoal,
    clearGoal: clearControllerGoal,
    clearSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    setEffort,
    switchTab,
    openProjectTab,
    openGlobalTab,
    closeTab,
    reorderTabs,
    syncActiveTab,
    ensureBlankTab,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [modesByTab, setModesByTab] = useState<Record<string, Mode>>({});
  const [collaborationModesByTab, setCollaborationModesByTab] = useState<Record<string, CollaborationMode>>({});
  const [toolApprovalModesByTab, setToolApprovalModesByTab] = useState<Record<string, ToolApprovalMode>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const [goalsByTab, setGoalsByTab] = useState<Record<string, string>>({});
  const [goalDraftModesByTab, setGoalDraftModesByTab] = useState<Record<string, boolean>>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
  const [startupUpdateChecksEnabled, setStartupUpdateChecksEnabled] = useState<boolean | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteSessions, setPaletteSessions] = useState<SessionMeta[]>([]);
  const { showToast } = useToast();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(true);
  const [rightDockTreeWidth, setRightDockTreeWidth] = useState(loadRightDockTreeWidth);
  const [rightDockPreviewWidth, setRightDockPreviewWidth] = useState(loadRightDockPreviewWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [rightDockMode, setRightDockMode] = useState<RightDockMode>("context");
  const [workspaceRevealRequest, setWorkspaceRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceChangeRevealRequest, setWorkspaceChangeRevealRequest] = useState<WorkspaceRevealRequest | null>(null);
  const [workspaceFileListRequest, setWorkspaceFileListRequest] = useState<WorkspaceFileListRequest | null>(null);
  const [workspaceChangeListRequest, setWorkspaceChangeListRequest] = useState<WorkspaceChangeListRequest | null>(null);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [transientOverlayDismissSignal, setTransientOverlayDismissSignal] = useState(0);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [expandThinking, setExpandThinking] = useState(false);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const [topicExportOpen, setTopicExportOpen] = useState(false);
  const [sidebarTogglePressed, setSidebarTogglePressed] = useState(false);
  const [workspaceTogglePressed, setWorkspaceTogglePressed] = useState(false);
  const [clearContextPending, setClearContextPending] = useState(false);
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);
  const appRef = useRef<HTMLDivElement>(null);
  const sidebarTogglePressTimerRef = useRef<number | null>(null);
  const workspaceTogglePressTimerRef = useRef<number | null>(null);

  // Persist window geometry across launches.
  useWindowStatePersistence();

  const closeTransientOverlays = useCallback(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  }, []);

  const pulseSidebarToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (sidebarTogglePressTimerRef.current !== null) {
      window.clearTimeout(sidebarTogglePressTimerRef.current);
    }
    setSidebarTogglePressed(true);
    sidebarTogglePressTimerRef.current = window.setTimeout(() => {
      sidebarTogglePressTimerRef.current = null;
      setSidebarTogglePressed(false);
    }, 260);
  }, []);

  const pulseWorkspaceToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (workspaceTogglePressTimerRef.current !== null) {
      window.clearTimeout(workspaceTogglePressTimerRef.current);
    }
    setWorkspaceTogglePressed(true);
    workspaceTogglePressTimerRef.current = window.setTimeout(() => {
      workspaceTogglePressTimerRef.current = null;
      setWorkspaceTogglePressed(false);
    }, 260);
  }, []);

  const anchorAppScrollToChat = useCallback(() => {
    if (typeof window === "undefined") return;
    const el = appRef.current;
    if (!el) return;
    const pin = () => {
      el.scrollLeft = 0;
    };
    pin();
    window.requestAnimationFrame(pin);
    window.setTimeout(pin, 300);
  }, []);

  useEffect(() => {
    return () => {
      if (sidebarTogglePressTimerRef.current !== null) {
        window.clearTimeout(sidebarTogglePressTimerRef.current);
      }
      if (workspaceTogglePressTimerRef.current !== null) {
        window.clearTimeout(workspaceTogglePressTimerRef.current);
      }
    };
  }, []);

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

  const applyDesktopPreferences = useCallback(
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopLanguage" | "checkUpdates">) => {
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyTheme(nextTheme, nextStyle, { persist: false });
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
      setStartupUpdateChecksEnabled(settings.checkUpdates !== false);
    },
    [setLocalePref],
  );

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
      applyDesktopPreferences(settings);
      setExpandThinking(settings.expandThinking);
      hydrateDisplayMode(settings.displayMode);
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
      setStartupUpdateChecksEnabled(true);
    });
    return () => {
      cancelled = true;
    };
  }, [applyDesktopPreferences]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget("general");
    });
  }, [closeTransientOverlays]);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const runningRef = useRef(state.running);
  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  const preferredWorkspacePanelWidth = rightDockDetailActive ? rightDockPreviewWidth : rightDockTreeWidth;
  const workspacePanelMinWidth = rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const chatReservedWidth = workspacePanelOpen && !workspacePanelMaximized ? CHAT_COMFORT_MIN_WIDTH : CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });

  const resolvedWorkspacePanelWidth = resolveWorkspacePanelWidth({
    open: workspacePanelOpen,
    maximized: workspacePanelMaximized,
    preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth,
    availableWidth: workspacePanelAvailableWidth,
  });

  const workspacePanelRenderable =
    workspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth >= RIGHT_DOCK_MIN_RENDER_WIDTH);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  useEffect(() => {
    let cancelled = false;
    if (!activeTab?.topicId) {
      setActiveTopicTurns(undefined);
      return () => {
        cancelled = true;
      };
    }
    void app.ListProjectTree()
      .then((tree) => {
        if (!cancelled) setActiveTopicTurns(activeTopicTurnsFromTree(asArray(tree), activeTab));
      })
      .catch(() => {
        if (!cancelled) setActiveTopicTurns(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab?.scope, activeTab?.topicId, activeTab?.workspaceRoot, projectRevision]);
  const sessionTurns = useMemo(() => {
    const visibleUserTurns = state.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
    const currentTabTurns = Math.max(state.checkpoints.length, visibleUserTurns);
    return currentTabTurns > 0 ? currentTabTurns : activeTopicTurns ?? 0;
  }, [activeTopicTurns, state.checkpoints.length, state.items]);
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const legacyMode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const goal = activeTabId ? goalsByTab[activeTabId] ?? state.meta?.goal ?? activeTab?.goal ?? "" : "";
  const goalDraftMode = activeTabId ? Boolean(goalDraftModesByTab[activeTabId]) : false;
  const collaborationMode = activeTabId
    ? displayedCollaborationMode({
        goalDraftMode,
        localMode: collaborationModesByTab[activeTabId],
        metaGoal: state.meta?.goal,
        tabMode: activeTab?.collaborationMode,
        goal,
        legacyMode,
      })
    : "normal";
  const toolApprovalMode = activeTabId
    ? toolApprovalModesByTab[activeTabId] ?? normalizeToolApprovalMode(state.meta?.toolApprovalMode ?? activeTab?.toolApprovalMode, legacyMode, state.meta?.autoApproveTools ?? state.meta?.bypass)
    : "ask";
  const controllerReady = state.meta?.ready === true;
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
  const setGoalDraftModeForTab = useCallback((tabId: string, enabled: boolean) => {
    setGoalDraftModesByTab((current) => {
      if (Boolean(current[tabId]) === enabled) return current;
      if (enabled) return { ...current, [tabId]: true };
      const next = { ...current };
      delete next[tabId];
      return next;
    });
  }, []);
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => ({
      ...tab,
      running: tab.id === visibleTabId ? tab.running || state.running : tab.running,
      mode: modesByTab[tab.id] ?? normalizeMode(tab.mode),
      collaborationMode: tabListCollaborationMode({
        goalDraftMode: Boolean(goalDraftModesByTab[tab.id]),
        localMode: collaborationModesByTab[tab.id],
        tabMode: tab.collaborationMode,
        tabGoal: goalsByTab[tab.id] ?? tab.goal,
        legacyMode: normalizeMode(tab.mode),
      }),
      toolApprovalMode: toolApprovalModesByTab[tab.id] ?? normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode), tab.toolApprovalMode === "yolo"),
      goal: goalsByTab[tab.id] ?? tab.goal ?? "",
      active: tab.id === visibleTabId,
    }));
  }, [collaborationModesByTab, goalDraftModesByTab, goalsByTab, modesByTab, state.running, tabMetas, tabOrderIds, toolApprovalModesByTab, visibleTabId]);

  useEffect(() => {
    const ids = tabMetas.map((tab) => tab.id);
    setTabOrderIds((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) {
        if (!next.includes(id)) next.push(id);
      }
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
  }, [tabMetas]);

  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    for (const id of Object.keys(yoloRestoreToolApprovalModesRef.current)) {
      if (!ids.has(id)) delete yoloRestoreToolApprovalModesRef.current[id];
    }
    setGoalDraftModesByTab((current) => {
      let changed = false;
      const next: Record<string, boolean> = {};
      for (const tab of tabMetas) {
        if (keepGoalDraftMode(Boolean(current[tab.id]), tab.goal)) {
          next[tab.id] = true;
        } else if (current[tab.id]) {
          changed = true;
        }
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        const mode = normalizeMode(tab.mode);
        next[tab.id] = mode;
        if (current[tab.id] !== mode) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setCollaborationModesByTab((current) => {
      let changed = false;
      const next: Record<string, CollaborationMode> = {};
      for (const tab of tabMetas) {
        const value = tabListCollaborationMode({
          goalDraftMode: keepGoalDraftMode(Boolean(goalDraftModesByTab[tab.id]), tab.goal),
          tabMode: tab.collaborationMode,
          tabGoal: tab.goal,
          legacyMode: normalizeMode(tab.mode),
        });
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setToolApprovalModesByTab((current) => {
      let changed = false;
      const next: Record<string, ToolApprovalMode> = {};
      for (const tab of tabMetas) {
        const value = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
    setGoalsByTab((current) => {
      let changed = false;
      const next: Record<string, string> = {};
      for (const tab of tabMetas) {
        const value = tab.goal ?? "";
        next[tab.id] = value;
        if (current[tab.id] !== value) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [goalDraftModesByTab, tabMetas]);

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  useEffect(() => {
    if (!activeTabId || !state.meta) return;
    const nextGoal = state.meta.goalStatus === "running" ? state.meta.goal ?? "" : "";
    if (nextGoal) setGoalDraftModeForTab(activeTabId, false);
    setGoalsByTab((current) => (current[activeTabId] === nextGoal ? current : { ...current, [activeTabId]: nextGoal }));
    setCollaborationModesByTab((current) => {
      const nextMode = metaSyncedCollaborationMode({ nextGoal, goalDraftMode, legacyMode });
      return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
    });
  }, [activeTabId, goalDraftMode, legacyMode, setGoalDraftModeForTab, state.meta]);

  const syncModeToController = useCallback((m: Mode) => setControllerMode(m), [setControllerMode]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve approval-gated tools while user decisions still wait).
  // normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      if (!activeTabId) return;
      const nextCollaborationMode: CollaborationMode = modeHasPlan(m) ? "plan" : "normal";
      const nextToolApprovalMode: ToolApprovalMode = modeHasAutoApproveTools(m) ? "yolo" : "ask";
      setGoalDraftModeForTab(activeTabId, false);
      setMode(m);
      setCollaborationModesByTab((current) => (current[activeTabId] === nextCollaborationMode ? current : { ...current, [activeTabId]: nextCollaborationMode }));
      setToolApprovalModesByTab((current) => (current[activeTabId] === nextToolApprovalMode ? current : { ...current, [activeTabId]: nextToolApprovalMode }));
      setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
      void syncModeToController(m);
    },
    [activeTabId, setGoalDraftModeForTab, setMode, syncModeToController],
  );
  const applyCollaborationMode = useCallback(
    (m: CollaborationMode) => {
      if (!activeTabId) return;
      if (m === "goal") {
        setGoalDraftModeForTab(activeTabId, true);
        setCollaborationModesByTab((current) => (current[activeTabId] === "goal" ? current : { ...current, [activeTabId]: "goal" }));
        setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
        void setControllerCollaborationMode("normal");
        return;
      }
      setGoalDraftModeForTab(activeTabId, false);
      setCollaborationModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      if (m === "normal" || m === "plan") {
        setGoalsByTab((current) => (current[activeTabId] ? { ...current, [activeTabId]: "" } : current));
      }
      setMode(modeFromAxes(m === "plan", toolApprovalMode === "yolo"));
      void setControllerCollaborationMode(m);
    },
    [activeTabId, setControllerCollaborationMode, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  const applyToolApprovalMode = useCallback(
    (m: ToolApprovalMode) => {
      if (!activeTabId) return;
      if (m === "yolo") {
        if (toolApprovalMode !== "yolo") {
          yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(toolApprovalMode);
        }
      } else {
        yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(m);
      }
      setToolApprovalModesByTab((current) => (current[activeTabId] === m ? current : { ...current, [activeTabId]: m }));
      setMode(modeFromAxes(collaborationMode === "plan", m === "yolo"));
      void setControllerToolApprovalMode(m);
    },
    [activeTabId, collaborationMode, setControllerToolApprovalMode, setMode, toolApprovalMode],
  );
  const toggleYoloApprovalMode = useCallback(() => {
    if (!activeTabId) return;
    const next = toggleYoloToolApprovalMode(
      toolApprovalMode,
      yoloRestoreToolApprovalModesRef.current[activeTabId],
    );
    if (next.restore) {
      yoloRestoreToolApprovalModesRef.current[activeTabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  }, [activeTabId, applyToolApprovalMode, toolApprovalMode]);
  const applyGoal = useCallback(
    (nextGoal: string) => {
      if (!activeTabId) return;
      const trimmed = nextGoal.trim();
      setGoalDraftModeForTab(activeTabId, false);
      setGoalsByTab((current) => (current[activeTabId] === trimmed ? current : { ...current, [activeTabId]: trimmed }));
      setCollaborationModesByTab((current) => {
        const nextMode = trimmed ? "goal" : "normal";
        return current[activeTabId] === nextMode ? current : { ...current, [activeTabId]: nextMode };
      });
      setMode(modeFromAxes(false, toolApprovalMode === "yolo"));
      void (trimmed ? setControllerGoal(trimmed) : clearControllerGoal());
    },
    [activeTabId, clearControllerGoal, setControllerGoal, setGoalDraftModeForTab, setMode, toolApprovalMode],
  );
  const startGoal = useCallback(
    (nextGoal: string) => {
      const trimmed = nextGoal.trim();
      if (!trimmed) return;
      applyGoal(trimmed);
      send(trimmed, `/goal ${trimmed}`);
    },
    [applyGoal, send],
  );
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCallback(() => {
    applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan");
  }, [applyCollaborationMode, collaborationMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      await setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
    },
    [collaborationMode, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, setModel, toolApprovalMode],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and the legacy
  // SetBypass binding was a harmless no-op.
  useEffect(() => {
    if (!controllerReady) return;
    void setControllerCollaborationMode(controllerCollaborationMode({ collaborationMode, goal }));
    void setControllerToolApprovalMode(toolApprovalMode);
    if (goal.trim()) void setControllerGoal(goal);
  }, [collaborationMode, controllerReady, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, toolApprovalMode]);

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
    async (format: "markdown" | "json" | "pdf" | "image") => {
      const base = safeFilename(sessionTitle);
      setTopicExportOpen(false);
      try {
        if (format === "json") {
          const path = await app.PickExportFile(`${base}.json`, "application/json");
          if (path) await app.SaveExportFile(path, getSessionJson(), false);
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const blob = await renderSessionPdfBlob(getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const blob = await renderSessionImageBlob(getSessionMarkdown());
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) await app.SaveExportFile(path, getSessionMarkdown(), false);
        }
      } catch (err) {
        console.error("Failed to export session", err);
      }
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    send(text);
  }, [pendingPlanRevision, send, state.running]);

  useEffect(() => {
    setClearContextPending(false);
  }, [activeTabId]);

  const cancelClearContext = useCallback(() => {
    setClearContextPending(false);
  }, []);

  const confirmClearContext = useCallback(async () => {
    setClearContextPending(false);
    try {
      await clearSession();
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  }, [clearSession, notice, t]);

  // Keep runningRef in sync so handleSend sees the latest running value
  // even inside a stale closure.
  useEffect(() => {
    runningRef.current = state.running;
  }, [state.running]);

  // handleSend intercepts slash commands that need a desktop-native action before
  // they reach the backend: "/model <ref>" rebuilds on that model, "/memory"
  // opens Settings, and "/clear" shows an in-app confirmation card. Everything else — skills (/init, …),
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
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
        setClearContextPending(true);
        return;
      }
      const goalCommand = /^\/goal(?:\s+(.*))?$/.exec(trimmed);
      if (goalCommand) {
        const arg = (goalCommand[1] ?? "").trim();
        if (arg && !["status", "clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          applyGoal(arg);
        } else if (["clear", "off", "stop", "done"].includes(arg.toLowerCase())) {
          applyGoal("");
        }
        send(trimmed, submitText.trim());
        return;
      }
      if (collaborationMode === "goal" && !goal.trim()) {
        applyGoal(trimmed);
        send(trimmed, `/goal ${submitText.trim()}`);
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
          const cur = getTheme();
          await app.SetDesktopAppearance(cur, arg);
          applyTheme(cur, arg);
          notice(t("settings.themeChanged", { theme: cur, style: arg }));
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      if (runningRef.current) { steer(submitText.trim()); return; }
      await setControllerCollaborationMode(collaborationMode);
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
      send(trimmed, submitText.trim());
    },
    [applyGoal, closeTransientOverlays, collaborationMode, goal, send, runShell, notice, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, steer, switchModel, t, toolApprovalMode],
  );

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    setTabMetas(tabs);
    return tabs;
  }, []);

  const blankSessionTarget = useCallback(() => {
    const activeWorkspaceRoot = activeTab?.scope === "project" ? activeTab.workspaceRoot || "" : "";
    const scope = activeWorkspaceRoot ? "project" : "global";
    return { scope, workspaceRoot: activeWorkspaceRoot };
  }, [activeTab?.scope, activeTab?.workspaceRoot]);

  const openBlankSession = useCallback(async (scope: string, workspaceRoot: string) => {
    await ensureBlankTab(scope, scope === "project" ? workspaceRoot : "");
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [ensureBlankTab, refreshTabMetas]);

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
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(el.getBoundingClientRect().height);
        if (Math.abs(footerHeightRef.current - next) < 2) return;
        footerHeightRef.current = next;
        setFooterHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);

  const toggleSidebar = useCallback(() => {
    closeTransientOverlays();
    pulseSidebarToggle();
    anchorAppScrollToChat();
    const nextCollapsed = !sidebarCollapsed;
    setSidebarCollapsed(nextCollapsed);
    saveSidebarCollapsed(nextCollapsed);
  }, [anchorAppScrollToChat, closeTransientOverlays, pulseSidebarToggle, sidebarCollapsed]);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    closeTransientOverlays();
    const next = clampSidebarWidth(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, [closeTransientOverlays]);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      event.preventDefault();
      closeTransientOverlays();
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
    [closeTransientOverlays, sidebarCollapsed, sidebarWidth],
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
      closeTransientOverlays();
      if (rightDockDetailActive) {
        const next = clampRightDockPreviewWidth(width);
        setRightDockPreviewWidth(next);
        saveRightDockPreviewWidth(next);
        return;
      }
      const next = clampRightDockTreeWidth(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockDetailActive],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockMode === "context") return;
      const next = clampRightDockPreviewWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [closeTransientOverlays, rightDockMode],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      closeTransientOverlays();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = workspacePanelRenderWidth;
      let nextDockWidth = startDockWidth;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        if (rightDockDetailActive) {
          setRightDockPreviewWidth(clampRightDockPreviewWidth(nextDockWidth));
        } else {
          setRightDockTreeWidth(clampRightDockTreeWidth(nextDockWidth));
        }
      };
      const onDone = () => {
        setSavedWorkspacePanelWidth(nextDockWidth);
        setWorkspacePanelResizing(false);
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
    [closeTransientOverlays, rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelRenderWidth],
  );

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      closeTransientOverlays();
      if (mode === "context" || mode !== rightDockMode) {
        setWorkspacePreviewActive(false);
      }
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // Keep file/change views docked; the rendered dock width is clamped to
        // the viewport so opening it reflows instead of forcing maximize.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
    },
    [closeTransientOverlays, rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    closeTransientOverlays();
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [closeTransientOverlays, workspacePanelOpen]);

  const toggleWorkspacePanel = useCallback(() => {
    pulseWorkspaceToggle();
    if (workspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    openWorkspacePanel("context");
  }, [closeWorkspacePanel, openWorkspacePanel, pulseWorkspaceToggle, workspacePanelRenderable]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  const openRightDockFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockFileList = useCallback(
    (paths: string[]) => {
      const normalized = Array.from(new Set(paths.map((path) => path.trim()).filter(Boolean)));
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceFileListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, paths: normalized }
          : null,
      );
      openWorkspacePanel("files");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeFile = useCallback(
    (path: string) => {
      const nextPath = path.trim();
      if (!nextPath) return;
      setWorkspaceRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest(null);
      setWorkspaceChangeRevealRequest((current) => ({ id: (current?.id ?? 0) + 1, path: nextPath }));
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const openRightDockChangeList = useCallback(
    (changes: WorkspaceChangeListEntry[]) => {
      const seen = new Set<string>();
      const normalized = changes
        .map((change) => ({ ...change, path: change.path.trim() }))
        .filter((change) => {
          if (!change.path || seen.has(change.path)) return false;
          seen.add(change.path);
          return true;
      });
      setWorkspaceRevealRequest(null);
      setWorkspaceChangeRevealRequest(null);
      setWorkspaceFileListRequest(null);
      setWorkspaceChangeListRequest((current) =>
        normalized.length > 0
          ? { id: (current?.id ?? 0) + 1, changes: normalized }
          : null,
      );
      openWorkspacePanel("changed");
    },
    [openWorkspacePanel],
  );

  const handleWorkspacePreviewModeChange = useCallback(
    (active: boolean) => {
      if (workspacePreviewActive === active) return;
      closeTransientOverlays();
      setWorkspacePreviewActive(active);
    },
    [closeTransientOverlays, workspacePreviewActive],
  );

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--chat-min-width": `${chatReservedWidth}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [chatReservedWidth, sidebarWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);

  const handleTabChange = useCallback(async (id: string) => {
    closeTransientOverlays();
    await switchTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, refreshTabMetas, switchTab]);

  const handleTabClose = useCallback(async (id: string) => {
    closeTransientOverlays();
    setModesByTab((current) => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    });
    setTabMetas((current) => {
      if (current.length <= 1) return current;
      const closingIndex = current.findIndex((tab) => tab.id === id);
      if (closingIndex < 0) return current;
      const closingTab = current[closingIndex];
      const remaining = current.filter((tab) => tab.id !== id);
      if (!closingTab.active && closingTab.id !== activeTabId) return remaining;
      const nextIndex = Math.min(closingIndex, remaining.length - 1);
      const nextActiveId = remaining[nextIndex]?.id;
      return remaining.map((tab) => ({ ...tab, active: tab.id === nextActiveId }));
    });
    await closeTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [activeTabId, closeTab, closeTransientOverlays, refreshTabMetas]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      await closeTab(id);
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      await switchTab(nextActiveTabId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTab, closeTransientOverlays, refreshTabMetas, switchTab, tabMetas]);

  const handleTabsReorder = useCallback(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, reorderTabs]);

  const handleNewTab = useCallback(async () => {
    closeTransientOverlays();
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  }, [blankSessionTarget, closeTransientOverlays, openBlankSession]);

  const handleMessageAction = useCallback(async (turn: number, scope: string) => {
    await rewind(turn, scope);
    if (scope === "fork") {
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      setTabRevealSignal((signal) => signal + 1);
      return;
    }
    if (scope === "code" || scope === "both") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
    }
  }, [refreshTabMetas, rewind]);

  const handleOpenTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    closeTransientOverlays();
    if (scope === "global") {
      await openGlobalTab(topicId);
    } else {
      await openProjectTab(workspaceRoot, topicId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, openGlobalTab, openProjectTab, refreshTabMetas]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    closeTransientOverlays();
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [closeTransientOverlays, listSessions]);
  const openAllHistory = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [closeTransientOverlays, listSessions]);
  const openTrash = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [closeTransientOverlays, listTrashedSessions]);
  const closeHistory = useCallback(() => {
    closeTransientOverlays();
    setHistView(null);
  }, [closeTransientOverlays]);

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
        await refreshTabMetas();
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

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up.
  const openPalette = useCallback(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
  }, [closeTransientOverlays, listSessions]);
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((cur) => {
          if (!cur) void openPalette();
          return cur;
        });
      } else if (e.key === "Escape") {
        setPaletteOpen(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [openPalette]);
  const paletteItems = useMemo<PaletteItem[]>(() => {
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), keywords: ["new", "新建"], run: () => void handleNewTab() },
      { id: "cmd-history", group: t("palette.group.commands"), title: t("palette.cmd.history"), keywords: ["history", "历史"], run: () => void openAllHistory() },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), keywords: ["settings", "设置"], run: () => setSettingsTarget("general") },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), keywords: ["memory", "记忆"], run: () => setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), keywords: ["model", "模型"], run: () => setSettingsTarget("models") },
    ];
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: s.title?.trim() || s.preview || t("history.emptySession"),
      hint: s.workspaceRoot || undefined,
      run: () => void onResumeSession(s),
    }));
    return [...cmds, ...sessionItems];
  }, [t, paletteSessions, handleNewTab, openAllHistory, openTrash, onResumeSession]);
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

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const workspacePanelResetWidth = rightDockDetailActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelMaxWidth = rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;
  const topicbarTitle = topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarSubtitleVisible = Boolean(topicbarWorkspaceLabel);
  const topicbarSubtitleTitle = topicbarWorkspacePath || topicbarWorkspaceLabel;

  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
    <div ref={appRef} className={["app", `app--${desktopPlatform}`, browserPreviewChrome ? "app--browser-preview" : ""].filter(Boolean).join(" ")}>
      <div
        className={[
          "layout",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        <AppChrome
          platform={desktopPlatform}
          browserPreviewChrome={browserPreviewChrome}
          tabs={visibleTabs}
          activeTabId={visibleTabId}
          revealActiveSignal={tabRevealSignal}
          commandCompact={workspacePanelGridOpen}
          sidebarTogglePressed={sidebarTogglePressed}
          sidebarExpandBlocked={sidebarExpandBlocked}
          sidebarCollapsed={sidebarCollapsed}
          sidebarToggleTitle={sidebarToggleTitle}
          workspacePanelMaximized={workspacePanelMaximized}
          workspacePanelRenderable={workspacePanelRenderable}
          workspaceTogglePressed={workspaceTogglePressed}
          workspacePanelLabel={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
          onToggleSidebar={toggleSidebar}
          onToggleWorkspacePanel={toggleWorkspacePanel}
          onTabChange={(id) => void handleTabChange(id)}
          onTabClose={(id) => void handleTabClose(id)}
          onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
          onTabsReorder={(ids) => void handleTabsReorder(ids)}
          onNewTab={() => void handleNewTab()}
          onOpenPalette={() => void openPalette()}
        />

        <aside className={`sidebar${sidebarCollapsed ? " sidebar--collapsed" : ""}`} aria-label={t("sidebar.navigation")}>
          <div className="sidebar__brand" aria-hidden={sidebarCollapsed}>
            <img src={logoWordmark} alt="Reasonix" className="sidebar__brand-logo" draggable={false} />
          </div>

          <button
            className="sidebar__new"
            onClick={() => {
              void handleNewTab();
            }}
          >
            <SquarePen size={18} />
            <span>{t("topbar.newSession")}</span>
          </button>

          <section className="sidebar__section sidebar__section--projects">
            <ProjectTree
              activeScope={activeTab?.scope}
              activeWorkspaceRoot={activeTab?.workspaceRoot}
              activeTopicId={activeTab?.topicId}
              onOpenTopic={handleOpenTopic}
              onOpenProjectHistory={openProjectHistory}
              onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
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
                onClick={() => {
                  closeTransientOverlays();
                  setSettingsTarget("general");
                }}
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
          <>
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
                  <h1 title={topicTitle(activeTab)}>{topicbarTitle}</h1>
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
              {topicbarSubtitleVisible && (
                <div className="topicbar__subtitle" title={topicbarSubtitleTitle}>
                  <span>{topicbarWorkspaceLabel}</span>
                </div>
              )}
            </div>
            <div className="topicbar__spacer" />
            <div className="topicbar__actions">
              <CopyButton
                getText={getSessionMarkdown}
                label={t("topicBar.copyAll")}
                showLabel={false}
                className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
              />
              <div className={`topicbar__export${topicExportOpen ? " topicbar__export--open" : ""}`}>
                <Tooltip label={t("topicBar.export")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
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
                    <button type="button" role="menuitem" onClick={() => void exportSession("markdown")}>
                      <FileText size={13} />
                      <span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("json")}>
                      <FileJson size={13} />
                      <span>{t("topicBar.exportJson")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("pdf")}>
                      <FileDown size={13} />
                      <span>{t("topicBar.exportPdf")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("image")}>
                      <FileImage size={13} />
                      <span>{t("topicBar.exportImage")}</span>
                    </button>
                  </div>
                )}
              </div>
              <Tooltip label={t("workspace.changedTab")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--label"
                  type="button"
                  aria-label={t("workspace.changedTab")}
                  aria-pressed={workspacePanelRenderable && rightDockMode === "changed"}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={14} />
                  <span>{t("workspace.changedTab")}</span>
                </button>
              </Tooltip>
              <Tooltip label={t("topicBar.command")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--label topicbar__action-btn--accent"
                  type="button"
                  aria-label={t("topicBar.command")}
                  onClick={() => void openPalette()}
                >
                  <Command size={14} />
                  <span>{t("topicBar.command")}</span>
                </button>
              </Tooltip>
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner enabled={startupUpdateChecksEnabled === true} />

          <main className="main">
            {state.meta?.ready === false && !state.meta?.startupErr ? (
              <div className="loading-screen">
                <div className="loading-screen__spinner" />
                <span className="loading-screen__text">{t("common.loading")}</span>
              </div>
            ) : (
              <Transcript
                items={state.items}
                live={state.live}
                footerHeight={footerHeight}
                onPrompt={send}
                onRewind={handleMessageAction}
                checkpoints={state.checkpoints}
                actionPending={state.messageAction != null}
                rewindDisabled={state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
                defaultExpandThinking={expandThinking}
              />
            )}
          </main>

          <footer className="footer" ref={footerRef}>
            {showTodos && <TodoPanel todos={todos} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
            {state.approval && (
              <ApprovalModal
                approval={state.approval}
                onAnswer={(allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; sync the
                  // tab-local indicator and persisted safe mode immediately.
                  if (state.approval!.tool === "exit_plan_mode" && allow) applyCollaborationMode("normal");
                  approve(state.approval!.id, allow, session, persist);
                }}
                onRevisePlan={(text) => {
                  setPendingPlanRevision(text);
                  approve(state.approval!.id, false, false, false);
                }}
                onExitPlan={() => {
                  applyCollaborationMode("normal");
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
            {clearContextPending && (
              <ClearContextCard
                onCancel={cancelClearContext}
                onConfirm={() => {
                  void confirmClearContext();
                }}
              />
            )}
            <Composer
              running={state.running}
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              goal={goal}
              cwd={state.meta?.cwd}
              modelLabel={state.meta?.label ?? t("status.connecting")}
              tabId={activeTabId}
              effort={state.effort}
              onSend={handleSend}
              onCancel={cancel}
              onCycleMode={cycleMode}
              onSetMode={applyMode}
              onSetCollaborationMode={applyCollaborationMode}
              onSetToolApprovalMode={applyToolApprovalMode}
              onToggleYoloApprovalMode={toggleYoloApprovalMode}
              onSetGoal={startGoal}
              onClearGoal={() => applyGoal("")}
              onSwitchModel={switchModel}
              onSetEffort={setEffort}
              insertRequest={composerInsertRequest}
              disabled={state.meta?.ready === false || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              decisionPending={state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              ready={state.meta?.ready === true}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              retry={state.retry}
              transientDismissSignal={transientOverlayDismissSignal}
            />
            <StatusBar
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              jobs={state.jobs}
              running={state.running}
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              sessionTurns={sessionTurns}
              sessionTokens={state.sessionTokens}
              turnTokens={state.turnTotalTokens}
              cost={state.sessionCost}
              currency={state.sessionCurrency}
              modelLabel={state.meta?.label}
            />
          </footer>
          </>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelResizeMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__tools">
              <div className="workbench-dock__tabs" role="tablist" aria-label={t("rightDock.views")}>
                {SHOW_CONTEXT_DOCK && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "context"}
                    className={`workbench-dock__tab${rightDockMode === "context" ? " workbench-dock__tab--active" : ""}`}
                    onClick={() => openRightDockMode("context")}
                  >
                    <Activity size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.overview")}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "files"}
                  className={`workbench-dock__tab${rightDockMode === "files" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("files")}
                >
                  <FileText size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.filesTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "changed"}
                  className={`workbench-dock__tab${rightDockMode === "changed" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.changedTab")}</span>
                </button>
              </div>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "context" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionTokens={state.sessionTokens}
                  sessionCost={state.sessionCost}
                  sessionCurrency={state.sessionCurrency}
                  refreshKey={dockRefreshKey}
                  onOpenWorkspaceMode={openRightDockMode}
                  onOpenWorkspaceFile={openRightDockFile}
                  onOpenWorkspaceFileList={openRightDockFileList}
                  onOpenWorkspaceChangeList={openRightDockChangeList}
                  onOpenWorkspaceChangeFile={openRightDockChangeFile}
                />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  cwd={state.meta?.cwd}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => {
                    closeTransientOverlays();
                    setWorkspacePanelMaximized((value) => !value);
                  }}
                  onPreviewModeChange={handleWorkspacePreviewModeChange}
                  onAddToChat={addWorkspaceTextToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  revealPathRequest={workspaceRevealRequest}
                  changeRevealRequest={workspaceChangeRevealRequest}
                  fileListRequest={workspaceFileListRequest}
                  changeListRequest={workspaceChangeListRequest}
                  showViewTabs={false}
                />
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
          onChanged={() => {
            void refreshMeta();
            void app.Settings()
              .then(applyDesktopPreferences)
              .catch((e) => console.warn("desktop preferences refresh failed", e));
          }}
        />
      )}

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        items={paletteItems}
        placeholder={t("palette.placeholder")}
        emptyText={t("palette.empty")}
      />

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}
    </div>
    </ShellExpandProvider>
  );
}
