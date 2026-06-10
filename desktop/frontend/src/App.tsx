import { lazy, Suspense, useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, FormEvent, KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Archive,
  ChevronDown,
  ChevronRight,
  CircleDotDashed,
  Code2,
  FileSearch,
  Folder,
  FolderOpen,
  GitBranch,
  GitCommitHorizontal,
  GitPullRequestCreateArrow,
  Globe2,
  Maximize2,
  MessageCircle,
  MonitorDot,
  MoreHorizontal,
  PanelBottom,
  PinOff,
  Plug,
  Plus,
  Search,
  Settings as SettingsIcon,
  Pencil,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  SlidersHorizontal,
  SquareTerminal,
  Workflow,
  X,
} from "lucide-react";
import logoSymbol from "./assets/logo-symbol.svg";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, t, useI18n, useT } from "./lib/i18n";
import { useController } from "./lib/useController";
import { app, onProjectTreeChanged, onTerminalEvent, openExternal } from "./lib/bridge";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { StatusBar } from "./components/StatusBar";
import { UpdateBanner } from "./components/UpdateBanner";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { TabBar } from "./components/TabBar";
import { ProjectTree } from "./components/ProjectTree";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./components/ContextMenu";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import type { CollaborationMode, ComposerInsertRequest, Mode, ProjectNode, SessionMeta, SettingsTab, TabMeta, TerminalInfo, ToolApprovalMode } from "./lib/types";
import { modeWithAutoApproveTools, modeWithPlan, normalizeCollaborationMode, normalizeMode, normalizeToolApprovalMode } from "./lib/types";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import { useRafThrottle } from "./lib/useRafThrottle";
import { sessionActivityTime } from "./lib/session";
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
import { useWindowStatePersistence } from "./lib/windowState";

const SIDEBAR_COLLAPSED_KEY = "reasonix.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_DEFAULT_RATIO = 0.18;
const SIDEBAR_MIN_WIDTH = 264;
const SIDEBAR_MAX_WIDTH = 300;
const CHAT_MIN_WIDTH = 400;
const WORKSPACE_RESIZER_WIDTH = 8;

const Transcript = lazy(() => import("./components/Transcript").then((m) => ({ default: m.Transcript })));
const HistoryPanel = lazy(() => import("./components/HistoryPanel").then((m) => ({ default: m.HistoryPanel })));
const SettingsPanel = lazy(() => import("./components/SettingsPanel").then((m) => ({ default: m.SettingsPanel })));
const ContextPanel = lazy(() => import("./components/ContextPanel").then((m) => ({ default: m.ContextPanel })));
const WorkspacePanel = lazy(() => import("./components/WorkspacePanel").then((m) => ({ default: m.WorkspacePanel })));
const PluginHub = lazy(() => import("./components/PluginHub").then((m) => ({ default: m.PluginHub })));
const SearchDialog = lazy(() => import("./components/SearchDialog").then((m) => ({ default: m.SearchDialog })));

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}
const CONTEXT_PANEL_MIN_WIDTH = 340;
const RIGHT_DOCK_MIN_WIDTH = CONTEXT_PANEL_MIN_WIDTH;
const RIGHT_DOCK_CONTEXT_WIDTH = 380;
const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 320;
const RIGHT_DOCK_TREE_DEFAULT_RATIO = 0.25;
const RIGHT_DOCK_TREE_MIN_WIDTH = 260;
const RIGHT_DOCK_TREE_MAX_WIDTH = 560;
const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 640;
const RIGHT_DOCK_MAX_WIDTH = 860;
const TERMINAL_BUFFER_LIMIT = 200_000;

type RightDockMode = "launcher" | "context" | "files" | "changed";
type MainSurface = "chat" | "plugins";
type SidebarSectionKey = "pinned" | "projects" | "recent";
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };

function initialMainSurface(): MainSurface {
  if (typeof window === "undefined") return "chat";
  return new URLSearchParams(window.location.search).get("surface") === "plugins" ? "plugins" : "chat";
}

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
  return normalizeMode(mode);
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

function terminalRootLabel(path: string): string {
  const normalized = path.replace(/\//g, "\\");
  const drive = normalized.match(/^[A-Za-z]:\\/);
  if (drive) return drive[0];
  if (normalized.startsWith("~")) return "~";
  const first = normalized.split("\\").filter(Boolean)[0];
  return first || "~";
}

function sessionSidebarTitle(session: SessionMeta): string {
  const title = session.title || session.topicTitle || session.preview || "";
  return title.trim() || "Untitled";
}

function sidebarAgeLabel(session: SessionMeta, locale: string): string {
  const time = sessionActivityTime(session);
  if (!time) return "";
  const delta = Math.max(0, Date.now() - time);
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  const week = 7 * day;
  const zh = locale === "zh";
  if (delta < minute) return zh ? "刚刚" : "now";
  if (delta < hour) {
    const n = Math.max(1, Math.round(delta / minute));
    return zh ? `${n} 分钟` : `${n}m`;
  }
  if (delta < day) {
    const n = Math.max(1, Math.round(delta / hour));
    return zh ? `${n} 小时` : `${n}h`;
  }
  if (delta < week) {
    const n = Math.max(1, Math.round(delta / day));
    return zh ? `${n} 天` : `${n}d`;
  }
  const n = Math.max(1, Math.round(delta / week));
  return zh ? `${n} 周` : `${n}w`;
}

function appendTerminalText(current: string, next: string): string {
  if (!next) return current;
  const merged = current + next;
  return merged.length > TERMINAL_BUFFER_LIMIT ? merged.slice(merged.length - TERMINAL_BUFFER_LIMIT) : merged;
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
    setCollaborationMode,
    setToolApprovalMode,
    setGoal,
    clearGoal,
    newSession,
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
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [mainSurface, setMainSurface] = useState<MainSurface>(initialMainSurface);
  const [recentSessions, setRecentSessions] = useState<SessionMeta[]>([]);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchSessions, setSearchSessions] = useState<SessionMeta[]>([]);
  const [searchLoading, setSearchLoading] = useState(false);
  const [sidebarProjectTree, setSidebarProjectTree] = useState<ProjectNode[]>([]);
  const [collapsedSidebarSections, setCollapsedSidebarSections] = useState<Record<SidebarSectionKey, boolean>>({
    pinned: false,
    projects: false,
    recent: false,
  });
  const [pinnedProjectMenu, setPinnedProjectMenu] = useState<{ root: string; label: string } | null>(null);
  const [pinnedProjectMenuPoint, setPinnedProjectMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [recentSessionMenu, setRecentSessionMenu] = useState<SessionMeta | null>(null);
  const [recentSessionMenuPoint, setRecentSessionMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(false);
  const [rightDockTreeWidth, setRightDockTreeWidth] = useState(loadRightDockTreeWidth);
  const [rightDockPreviewWidth, setRightDockPreviewWidth] = useState(loadRightDockPreviewWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [rightDockMode, setRightDockMode] = useState<RightDockMode>("launcher");
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [environmentPanelOpen, setEnvironmentPanelOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [terminalDraft, setTerminalDraft] = useState("");
  const [terminalSession, setTerminalSession] = useState<TerminalInfo | null>(null);
  const [terminalOutput, setTerminalOutput] = useState("");
  const [terminalStarting, setTerminalStarting] = useState(false);
  const [terminalExited, setTerminalExited] = useState(false);
  const [projectRevision, setProjectRevision] = useState(0);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const setSidebarWidthRaf = useRafThrottle((width: number) => setSidebarWidth(width));
  const setRightDockPreviewWidthRaf = useRafThrottle((width: number) => setRightDockPreviewWidth(width));
  const setRightDockTreeWidthRaf = useRafThrottle((width: number) => setRightDockTreeWidth(width));
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);
  const terminalSessionIdRef = useRef("");
  const terminalBodyRef = useRef<HTMLDivElement>(null);
  const sidebarScrollRef = useRef<HTMLDivElement>(null);
  const sidebarScrollDragRef = useRef<{ pointerId: number; startY: number; startScrollTop: number; scrollPerPx: number } | null>(null);
  const [sidebarScrollThumb, setSidebarScrollThumb] = useState({
    visible: false,
    top: 0,
    height: 0,
    trackTop: 0,
    trackHeight: 0,
  });

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

  const refreshRecentSessions = useCallback(async () => {
    const sessions = await listSessions();
    setRecentSessions(sessions.slice(0, 5));
  }, [listSessions]);

  const closeTransientOverlays = useCallback(() => {
    setSearchOpen(false);
    setSettingsTarget(null);
    setHistView(null);
    setPinnedProjectMenu(null);
    setRecentSessionMenu(null);
  }, []);

  useEffect(() => {
    void refreshRecentSessions();
  }, [activeTabId, histView, projectRevision, refreshRecentSessions, state.running]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget("general");
    });
  }, [closeTransientOverlays]);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const layoutRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLElement>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const preferredWorkspacePanelWidth =
    rightDockMode === "context"
      ? RIGHT_DOCK_CONTEXT_WIDTH
      : workspacePreviewActive
      ? rightDockPreviewWidth
      : rightDockTreeWidth;
  const sidebarRenderWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const measuredMainWidth = layoutWidth > 0 ? Math.max(0, layoutWidth - sidebarRenderWidth) : CHAT_MIN_WIDTH + WORKSPACE_RESIZER_WIDTH + preferredWorkspacePanelWidth;
  const workspacePanelMinWidth = workspacePreviewActive ? RIGHT_DOCK_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
  const pluginSurface = mainSurface === "plugins";
  const homeEmpty = !pluginSurface && !sessionHasContent && state.meta?.ready !== false && !state.meta?.startupErr;
  const chatWorkbenchActive = !pluginSurface && !homeEmpty;

  const budget = Math.max(0, measuredMainWidth - CHAT_MIN_WIDTH - WORKSPACE_RESIZER_WIDTH);
  const workspacePanelFloating = chatWorkbenchActive && workspacePanelOpen && !workspacePanelMaximized && budget < workspacePanelMinWidth;

  const resolvedWorkspacePanelWidth = chatWorkbenchActive && workspacePanelOpen && !workspacePanelMaximized
    ? (workspacePanelFloating ? Math.min(measuredMainWidth, Math.max(workspacePanelMinWidth, preferredWorkspacePanelWidth)) : resolveRightDockWidth(measuredMainWidth, preferredWorkspacePanelWidth, workspacePanelMinWidth))
    : preferredWorkspacePanelWidth;

  const workspacePanelRenderable = chatWorkbenchActive && workspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth > 0);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized && !workspacePanelFloating;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const terminalCwd = terminalSession?.cwd || activeTab?.workspaceRoot || state.meta?.cwd || "";
  const terminalPath = terminalCwd ? terminalCwd.replace(/\//g, "\\") : "~";
  const terminalTabTitle = terminalSession?.shell
    ? `${terminalSession.shell}: ${terminalRootLabel(terminalPath)}`
    : desktopPlatform === "windows"
    ? `Administrator: ${terminalRootLabel(terminalPath)}`
    : terminalPath;
  const terminalPrompt = desktopPlatform === "windows" ? `PS ${terminalPath}>` : `${terminalPath} $`;
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const mode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const collaborationMode = normalizeCollaborationMode(state.meta?.collaborationMode, state.meta?.goal, mode);
  const toolApprovalMode = normalizeToolApprovalMode(state.meta?.toolApprovalMode, mode, state.meta?.autoApproveTools || state.meta?.bypass);
  const currentTurnCount = useMemo(() => state.items.reduce((count, item) => count + (item.kind === "user" ? 1 : 0), 0), [state.items]);
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
  const topicbarProjectPrefix = activeTab ? tabWorkspaceTitle(activeTab) : "";
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => ({
      ...tab,
      mode: modesByTab[tab.id] ?? normalizeModeValue(tab.mode),
      active: tab.id === visibleTabId,
    }));
  }, [modesByTab, tabMetas, tabOrderIds, visibleTabId]);
  const sidebarPinnedProjects = useMemo(() => {
    return sidebarProjectTree
      .filter((node) => node.kind === "project" && Boolean(node.root) && node.pinned)
      .map((node) => ({
        root: node.root!,
        label: node.label || workspaceDisplayName(node.root),
      }));
  }, [sidebarProjectTree]);
  const sidebarPinnedRoots = useMemo(() => sidebarPinnedProjects.map((project) => project.root), [sidebarPinnedProjects]);

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
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        const mode = normalizeModeValue(tab.mode);
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
  const applyCollaborationMode = useCallback(
    (next: CollaborationMode) => {
      setMode((current) => modeWithPlan(current, next === "plan"));
      void setCollaborationMode(next);
    },
    [setMode, setCollaborationMode],
  );
  const applyToolApprovalMode = useCallback(
    (next: ToolApprovalMode) => {
      setMode((current) => modeWithAutoApproveTools(current, next === "yolo"));
      void setToolApprovalMode(next);
    },
    [setMode, setToolApprovalMode],
  );
  const applyGoal = useCallback(
    (goal: string) => {
      const trimmed = goal.trim();
      if (!trimmed) return;
      setMode((current) => modeWithPlan(current, false));
      void setGoal(trimmed);
    },
    [setMode, setGoal],
  );
  const clearActiveGoal = useCallback(() => {
    setMode((current) => modeWithPlan(current, false));
    void clearGoal();
  }, [setMode, clearGoal]);

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

  // useDeferredValue lets React prioritise Composer input (high-priority) over
  // Transcript re-renders (low-priority) during streaming. When a keystroke
  // and a transcript update collide, the keystroke is processed immediately
  // and the transcript re-render is deferred to idle time.
  const deferredItems = useDeferredValue(state.items);
  const sessionTitle = topicTitle(activeTab);

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
    setMainSurface("chat");
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
        setSidebarWidthRaf(nextWidth);
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
    [setSidebarWidthRaf, sidebarCollapsed, sidebarWidth],
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

  const updateSidebarScrollThumb = useCallback(() => {
    const el = sidebarScrollRef.current;
    if (!el) {
      setSidebarScrollThumb((current) => (current.visible ? { visible: false, top: 0, height: 0, trackTop: 0, trackHeight: 0 } : current));
      return;
    }
    const maxScroll = Math.max(0, el.scrollHeight - el.clientHeight);
    if (maxScroll <= 1 || el.clientHeight <= 0) {
      setSidebarScrollThumb((current) => (current.visible ? { visible: false, top: 0, height: 0, trackTop: 0, trackHeight: 0 } : current));
      return;
    }
    const desiredTrackTop = Math.min(184, Math.max(132, Math.round(el.clientHeight * 0.3)));
    const trackBottom = Math.min(64, Math.max(40, Math.round(el.clientHeight * 0.08)));
    const minTrackHeight = Math.min(120, Math.max(72, el.clientHeight - trackBottom));
    const trackTop = Math.min(desiredTrackTop, Math.max(0, el.clientHeight - minTrackHeight - trackBottom));
    const trackHeight = Math.max(minTrackHeight, el.clientHeight - trackTop - trackBottom);
    const height = Math.max(36, Math.round((el.clientHeight / el.scrollHeight) * trackHeight));
    const maxTop = Math.max(0, trackHeight - height);
    const top = Math.round((el.scrollTop / maxScroll) * maxTop);
    setSidebarScrollThumb((current) =>
      current.visible === true &&
      current.top === top &&
      current.height === height &&
      current.trackTop === trackTop &&
      current.trackHeight === trackHeight
        ? current
        : { visible: true, top, height, trackTop, trackHeight },
    );
  }, []);

  useEffect(() => {
    const el = sidebarScrollRef.current;
    updateSidebarScrollThumb();
    if (!el) return;
    let frame = 0;
    const schedule = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(updateSidebarScrollThumb);
    };
    el.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    const resizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    resizeObserver?.observe(el);
    for (const child of Array.from(el.children)) {
      resizeObserver?.observe(child);
    }
    schedule();
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      el.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      resizeObserver?.disconnect();
    };
  }, [
    collapsedSidebarSections,
    mainSurface,
    projectRevision,
    recentSessions.length,
    searchOpen,
    sidebarCollapsed,
    sidebarPinnedProjects.length,
    sidebarProjectTree.length,
    updateSidebarScrollThumb,
  ]);

  const beginSidebarScrollThumbDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const el = sidebarScrollRef.current;
    if (!el || !sidebarScrollThumb.visible) return;
    event.preventDefault();
    event.stopPropagation();
    const maxThumbTop = Math.max(1, sidebarScrollThumb.trackHeight - sidebarScrollThumb.height);
    const maxScroll = Math.max(1, el.scrollHeight - el.clientHeight);
    sidebarScrollDragRef.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      startScrollTop: el.scrollTop,
      scrollPerPx: maxScroll / maxThumbTop,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }, [sidebarScrollThumb.height, sidebarScrollThumb.trackHeight, sidebarScrollThumb.visible]);

  const moveSidebarScrollThumb = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = sidebarScrollDragRef.current;
    const el = sidebarScrollRef.current;
    if (!drag || !el || drag.pointerId !== event.pointerId) return;
    event.preventDefault();
    el.scrollTop = drag.startScrollTop + (event.clientY - drag.startY) * drag.scrollPerPx;
  }, []);

  const endSidebarScrollThumbDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = sidebarScrollDragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    sidebarScrollDragRef.current = null;
    event.currentTarget.releasePointerCapture(event.pointerId);
  }, []);

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      if (rightDockMode === "context") return;
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
    [rightDockMode, workspacePreviewActive],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      if (rightDockMode === "context") return;
      const next = clampRightDockWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [rightDockMode],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = preferredWorkspacePanelWidth;
      let nextDockWidth = startDockWidth;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        if (rightDockMode === "context") return;
        if (workspacePreviewActive) {
          setRightDockPreviewWidthRaf(clampRightDockWidth(nextDockWidth));
        } else {
          setRightDockTreeWidthRaf(clampRightDockTreeWidth(nextDockWidth));
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
    [preferredWorkspacePanelWidth, rightDockMode, setRightDockPreviewWidthRaf, setRightDockTreeWidthRaf, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePreviewActive],
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

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // When user explicitly opens the panel, we do NOT force maximize.
        // If there's not enough room, the panel will open in floating mode
        // over the chat area, preserving the chat area's minimum width
        // and keeping the panel's close button accessible.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
    },
    [rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [workspacePanelOpen]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  const openBrowserFromDock = useCallback(() => {
    const url = window.prompt(t("rightDock.browserPrompt"), "https://");
    const trimmed = url?.trim();
    if (!trimmed) return;
    const normalized = /^https?:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`;
    openExternal(normalized);
  }, [t]);

  const openBottomTerminal = useCallback(() => {
    setTerminalOpen(true);
    setWorkspacePanelMaximized(false);
  }, []);

  const stopTerminalSession = useCallback(() => {
    const sessionID = terminalSessionIdRef.current;
    terminalSessionIdRef.current = "";
    setTerminalSession(null);
    setTerminalStarting(false);
    setTerminalExited(false);
    if (sessionID) {
      app.TerminalStop(sessionID).catch(() => {});
    }
  }, []);

  const submitTerminalCommand = useCallback((event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    const command = terminalDraft.trim();
    if (!command) return;
    const sessionID = terminalSessionIdRef.current;
    if (!sessionID) {
      setTerminalOutput((current) => appendTerminalText(current, `${terminalPrompt} ${command}\nTerminal is not connected.\n`));
      setTerminalDraft("");
      setTerminalOpen(true);
      return;
    }
    setTerminalDraft("");
    setTerminalOpen(true);
    setTerminalOutput((current) => appendTerminalText(current, `${terminalPrompt} ${command}\n`));
    app.TerminalWrite(sessionID, `${command}\n`).catch((error) => {
      const message = error instanceof Error ? error.message : String(error);
      setTerminalOutput((current) => appendTerminalText(current, `Terminal write failed: ${message}\n`));
    });
  }, [terminalDraft, terminalPrompt]);

  useEffect(() => {
    return onTerminalEvent((event) => {
      const currentSessionID = terminalSessionIdRef.current;
      if (currentSessionID && event.sessionId !== currentSessionID) return;
      if (event.kind === "started") {
        terminalSessionIdRef.current = event.sessionId;
        setTerminalSession({
          sessionId: event.sessionId,
          cwd: event.cwd || terminalCwd,
          shell: event.shell || terminalSession?.shell || t("rightDock.terminal"),
        });
        setTerminalExited(false);
        return;
      }
      if (event.kind === "output") {
        setTerminalOutput((current) => appendTerminalText(current, event.data || ""));
        return;
      }
      if (event.kind === "exit") {
        if (event.err) {
          setTerminalOutput((current) => appendTerminalText(current, `\n${event.err}\n`));
        }
        terminalSessionIdRef.current = "";
        setTerminalExited(true);
      }
    });
  }, [terminalCwd, terminalSession?.shell]);

  useEffect(() => {
    if (!terminalOpen || homeEmpty || terminalStarting || terminalSessionIdRef.current) return;
    let alive = true;
    setTerminalStarting(true);
    setTerminalExited(false);
    setTerminalOutput("");
    app
      .TerminalStart({
        tabId: activeTabId || "",
        workspaceRoot: activeTab?.workspaceRoot || state.meta?.cwd || "",
        cols: 100,
        rows: 24,
      })
      .then((info) => {
        if (!alive) {
          app.TerminalStop(info.sessionId).catch(() => {});
          return;
        }
        terminalSessionIdRef.current = info.sessionId;
        setTerminalSession(info);
      })
      .catch((error) => {
        const message = error instanceof Error ? error.message : String(error);
        if (alive) {
          setTerminalOutput((current) => appendTerminalText(current, `Terminal failed to start: ${message}\n`));
          setTerminalExited(true);
        }
      })
      .finally(() => {
        if (alive) setTerminalStarting(false);
      });
    return () => {
      alive = false;
    };
  }, [activeTab?.workspaceRoot, activeTabId, homeEmpty, state.meta?.cwd, terminalOpen, terminalStarting]);

  useEffect(() => {
    if (terminalOpen) return;
    stopTerminalSession();
    setTerminalOutput("");
  }, [stopTerminalSession, terminalOpen]);

  useEffect(() => {
    const el = terminalBodyRef.current;
    if (!el || !terminalOpen) return;
    el.scrollTop = el.scrollHeight;
  }, [terminalOpen, terminalOutput]);

  useEffect(() => {
    if (pluginSurface || searchOpen || settingsTarget) return;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const mod = event.ctrlKey || event.metaKey;
      if (!mod) return;
      const key = event.key.toLowerCase();
      if (key === "p") {
        event.preventDefault();
        openWorkspacePanel("files");
      } else if (event.shiftKey && key === "g") {
        event.preventDefault();
        openWorkspacePanel("changed");
      } else if (key === "`") {
        event.preventDefault();
        setTerminalOpen((open) => !open);
      } else if (key === "t") {
        event.preventDefault();
        openBrowserFromDock();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [openBrowserFromDock, openWorkspacePanel, pluginSurface, searchOpen, settingsTarget]);

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
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);

  const handleTabChange = useCallback(async (id: string) => {
    setMainSurface("chat");
    await switchTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, switchTab]);

  const handleTabClose = useCallback(async (id: string) => {
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
  }, [activeTabId, closeTab, refreshTabMetas]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
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
  }, [closeTab, refreshTabMetas, switchTab, tabMetas]);

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
    setMainSurface("chat");
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
    setMainSurface("chat");
    if (scope === "global") {
      await openGlobalTab(topicId);
    } else {
      await openProjectTab(workspaceRoot, topicId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [openGlobalTab, openProjectTab, refreshTabMetas]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [listSessions]);
  const closePinnedProjectMenu = useCallback(() => {
    setPinnedProjectMenu(null);
    setPinnedProjectMenuPoint(null);
  }, []);
  const closeRecentSessionMenu = useCallback(() => {
    setRecentSessionMenu(null);
    setRecentSessionMenuPoint(null);
  }, []);
  const openPinnedProjectMenu = useCallback((
    event: ReactMouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>,
    project: { root: string; label: string },
  ) => {
    event.preventDefault();
    event.stopPropagation();
    setPinnedProjectMenu(project);
    setPinnedProjectMenuPoint(contextMenuPointFromEvent(event));
  }, []);
  const openRecentSessionMenu = useCallback((
    event: ReactMouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>,
    session: SessionMeta,
  ) => {
    event.preventDefault();
    event.stopPropagation();
    setPinnedProjectMenu(null);
    setPinnedProjectMenuPoint(null);
    setRecentSessionMenu(session);
    setRecentSessionMenuPoint(contextMenuPointFromEvent(event));
  }, []);
  const toggleSidebarSection = useCallback((section: SidebarSectionKey) => {
    setCollapsedSidebarSections((current) => ({ ...current, [section]: !current[section] }));
    if (section === "pinned") {
      setPinnedProjectMenu(null);
      setPinnedProjectMenuPoint(null);
    }
    if (section === "recent") {
      setRecentSessionMenu(null);
      setRecentSessionMenuPoint(null);
    }
  }, []);
  const openSearchDialog = useCallback(async () => {
    setHistView(null);
    setSearchOpen(true);
    setSearchLoading(true);
    try {
      setSearchSessions(await listSessions());
    } catch (e) {
      console.warn("search sessions load failed", e);
      setSearchSessions([]);
    } finally {
      setSearchLoading(false);
    }
  }, [listSessions]);
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "g") return;
      event.preventDefault();
      void openSearchDialog();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [openSearchDialog]);
  const closeHistory = useCallback(() => setHistView(null), []);
  const onResumeSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running) return;
      setMainSurface("chat");
      setHistView(null);
      setSearchOpen(false);
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      let targetTab: TabMeta | undefined;
      if (scope === "project" && session.workspaceRoot && session.topicId) {
        targetTab = await openProjectTab(session.workspaceRoot, session.topicId);
      } else if (scope === "global" && session.topicId) {
        targetTab = await openGlobalTab(session.topicId);
      }
      await resumeSession(session.path, targetTab?.id);
      if (targetTab) {
        await refreshTabMetas();
        setTabRevealSignal((signal) => signal + 1);
      }
    },
    [openGlobalTab, openProjectTab, refreshTabMetas, state.running, resumeSession],
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
  const renameRecentSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running) return;
      const currentTitle = sessionSidebarTitle(session);
      const next = window.prompt(t("history.rename"), currentTitle);
      closeRecentSessionMenu();
      if (next === null) return;
      const title = next.trim();
      if (!title || title === currentTitle) return;
      await renameSession(session.path, title);
      const sessions = await listSessions();
      setRecentSessions(sessions.slice(0, 5));
    },
    [closeRecentSessionMenu, listSessions, renameSession, state.running, t],
  );
  const deleteRecentSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running || session.current) return;
      closeRecentSessionMenu();
      await deleteSession(session.path);
      const sessions = await listSessions();
      setRecentSessions(sessions.slice(0, 5));
    },
    [closeRecentSessionMenu, deleteSession, listSessions, state.running],
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
      setMainSurface("chat");
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

  const setProjectPinned = useCallback(async (path: string, pinned: boolean) => {
    await app.SetProjectPinned(path, pinned);
    setPinnedProjectMenu(null);
    setPinnedProjectMenuPoint(null);
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
  }, [refreshTabMetas]);

  const renamePinnedProject = useCallback(async (path: string, label: string) => {
    const next = window.prompt(t("projectTree.renameProject"), label);
    if (next === null) return;
    await app.RenameProject(path, next);
    setPinnedProjectMenu(null);
    setPinnedProjectMenuPoint(null);
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
  }, [refreshTabMetas, t]);

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
  const workspacePanelResetWidth = rightDockMode === "context"
    ? RIGHT_DOCK_CONTEXT_WIDTH
    : workspacePreviewActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : defaultRightDockTreeWidth();
  const workspacePanelMaxWidth = workspacePreviewActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;
  const threadMode = chatWorkbenchActive;
  const environmentWorkspaceLabel =
    activeTab?.workspaceName || workspaceDisplayName(activeTab?.workspaceRoot) || workspaceDisplayName(state.meta?.cwd) || t("environment.local");
  const environmentThreadLabel = activeTab?.topicTitle || sessionTitle || t("environment.thread");
  const environmentPanel = (
    <aside id="thread-environment-panel" className="thread-env-card thread-env-card--rail" aria-label={t("environment.title")}>
      <div className="thread-env-card__head">
        <h2>{t("environment.title")}</h2>
        <button type="button" onClick={() => setSettingsTarget("general")} aria-label={t("environment.settings")}>
          <SettingsIcon size={17} />
        </button>
      </div>
      <div className="thread-env-card__group">
        <button type="button" className="thread-env-card__row" data-kind="changes" onClick={() => openWorkspacePanel("changed")}>
          <CircleDotDashed size={16} />
          <span>{t("environment.changes")}</span>
        </button>
        <button type="button" className="thread-env-card__row" data-kind="workspace" onClick={() => openWorkspacePanel("files")}>
          <MonitorDot size={16} />
          <span>{environmentWorkspaceLabel}</span>
          <ChevronDown size={14} />
        </button>
        <button
          type="button"
          className="thread-env-card__row"
          data-kind="branch"
          onClick={() => void openProjectHistory(activeTab?.scope === "project" ? "project" : "global", activeTab?.workspaceRoot || "")}
        >
          <GitBranch size={16} />
          <span>{environmentThreadLabel}</span>
          <ChevronDown size={14} />
        </button>
        <button type="button" className="thread-env-card__row" data-kind="commit" onClick={() => openWorkspacePanel("changed")}>
          <GitCommitHorizontal size={16} />
          <span>{t("environment.submitPush")}</span>
        </button>
        <button type="button" className="thread-env-card__row" data-kind="pullRequest" onClick={() => openExternal("https://github.com/esengine/DeepSeek-Reasonix/pulls")}>
          <GitPullRequestCreateArrow size={16} />
          <span>{t("environment.createPr")}</span>
        </button>
      </div>
      <div className="thread-env-card__sources">
        <strong>{t("environment.sources")}</strong>
        <span><Globe2 size={16} aria-hidden="true" />{t("environment.noSources")}</span>
      </div>
    </aside>
  );
  const pinnedProjectMenuItems = useMemo<ContextMenuItem[]>(() => {
    if (!pinnedProjectMenu) return [];
    return [
      {
        key: "unpin",
        icon: <PinOff size={18} />,
        label: t("projectTree.unpinProject"),
        onSelect: () => void setProjectPinned(pinnedProjectMenu.root, false),
      },
      {
        key: "reveal",
        icon: <FolderOpen size={18} />,
        label: desktopPlatform === "darwin"
          ? t("projectTree.revealInFinder")
          : desktopPlatform === "windows"
          ? t("projectTree.revealInExplorer")
          : t("projectTree.revealInFileManager"),
        onSelect: () => {
          closePinnedProjectMenu();
          void app.RevealPath(pinnedProjectMenu.root);
        },
      },
      {
        key: "rename",
        icon: <Pencil size={18} />,
        label: t("projectTree.renameProject"),
        onSelect: () => void renamePinnedProject(pinnedProjectMenu.root, pinnedProjectMenu.label),
      },
      {
        key: "history",
        icon: <Archive size={18} />,
        label: t("projectTree.archiveChats"),
        onSelect: () => {
          closePinnedProjectMenu();
          void openProjectHistory("project", pinnedProjectMenu.root);
        },
      },
      {
        key: "remove",
        icon: <X size={18} />,
        label: t("projectTree.remove"),
        danger: true,
        onSelect: () => {
          closePinnedProjectMenu();
          void removeWorkspace(pinnedProjectMenu.root);
        },
      },
    ];
  }, [closePinnedProjectMenu, desktopPlatform, openProjectHistory, pinnedProjectMenu, removeWorkspace, renamePinnedProject, setProjectPinned, t]);
  const recentSessionMenuItems = useMemo<ContextMenuItem[]>(() => {
    if (!recentSessionMenu) return [];
    return [
      {
        key: "open",
        icon: <MessageCircle size={18} />,
        label: t("history.openSession"),
        disabled: state.running,
        onSelect: () => {
          const target = recentSessionMenu;
          closeRecentSessionMenu();
          void onResumeSession(target);
        },
      },
      {
        key: "rename",
        icon: <Pencil size={18} />,
        label: t("history.rename"),
        disabled: state.running,
        onSelect: () => void renameRecentSession(recentSessionMenu),
      },
      {
        key: "archive",
        icon: <Archive size={18} />,
        label: t("projectTree.archiveChat"),
        disabled: state.running,
        onSelect: () => {
          closeRecentSessionMenu();
          setMainSurface("chat");
          setSearchOpen(false);
          setHistView({ kind: "history", source: "all", sessions: recentSessions });
        },
      },
      {
        key: "remove",
        icon: <X size={18} />,
        label: t("projectTree.removeChat"),
        disabled: state.running || recentSessionMenu.current,
        danger: true,
        onSelect: () => void deleteRecentSession(recentSessionMenu),
      },
    ];
  }, [
    closeRecentSessionMenu,
    deleteRecentSession,
    onResumeSession,
    recentSessionMenu,
    recentSessions,
    renameRecentSession,
    state.running,
    t,
  ]);

  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <div
      className={[
        "app",
        `app--${desktopPlatform}`,
        homeEmpty ? "app--home" : "",
        threadMode ? "app--thread" : "",
        environmentPanelOpen && chatWorkbenchActive ? "app--environment-panel-open" : "",
        pluginSurface ? "app--plugins" : "",
      ].join(" ")}
    >
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
        <header className="app-chrome">
          <div className="app-chrome__identity" aria-label="Reasonix">
            <img src={logoSymbol} alt="" className="app-chrome__mark" />
            <strong>Reasonix</strong>
          </div>
          <nav className="app-chrome__menu" aria-label={t("appMenu.navigation")}>
            <button
              type="button"
              onClick={() => {
                if (state.running) cancel();
                void startNewSession();
              }}
            >
              {t("appMenu.file")}
            </button>
            <button type="button" onClick={() => setSettingsTarget("general")}>
              {t("appMenu.edit")}
            </button>
            <button type="button" onClick={workspacePanelRenderable ? closeWorkspacePanel : () => openWorkspacePanel("files")}>
              {t("appMenu.view")}
            </button>
            <button
              type="button"
              onClick={sidebarExpandBlocked ? undefined : toggleSidebar}
              aria-disabled={sidebarExpandBlocked}
            >
              {t("appMenu.window")}
            </button>
            <button type="button" onClick={() => openExternal("https://github.com/esengine/DeepSeek-Reasonix")}>
              {t("appMenu.help")}
            </button>
          </nav>
          <div className="app-chrome__spacer" />
          <div className="app-chrome__window-actions">
            <button
              type="button"
              onClick={sidebarExpandBlocked ? undefined : toggleSidebar}
              aria-label={sidebarToggleTitle}
              aria-disabled={sidebarExpandBlocked}
            >
              {sidebarCollapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />}
            </button>
            <button
              type="button"
              onClick={workspacePanelRenderable ? closeWorkspacePanel : () => openWorkspacePanel("files")}
              aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
              aria-pressed={workspacePanelRenderable}
            >
              {workspacePanelRenderable ? <PanelRightClose size={15} /> : <PanelRightOpen size={15} />}
            </button>
          </div>
        </header>

        <aside className={`sidebar${sidebarCollapsed ? " sidebar--collapsed" : ""}`} aria-label={t("sidebar.navigation")}>
          <div className="sidebar__scroll-shell">
          <div ref={sidebarScrollRef} className="sidebar__scroll">
            <nav className="sidebar__primary" aria-label={t("sidebar.navigation")}>
              <Tooltip label={t("sidebar.quickChat")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className={`sidebar__navitem${homeEmpty && !searchOpen ? " sidebar__navitem--active" : ""}`}
                  type="button"
                  onClick={() => {
                    if (state.running) cancel();
                    void startNewSession();
                  }}
                >
                  <MessageCircle size={18} />
                  <span>{t("sidebar.quickChat")}</span>
                </button>
              </Tooltip>
              <Tooltip label={t("sidebar.search")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className={`sidebar__navitem sidebar__navitem--search${searchOpen ? " sidebar__navitem--active" : ""}`}
                  type="button"
                  onClick={() => void openSearchDialog()}
                >
                  <Search size={18} />
                  <span>{t("sidebar.search")}</span>
                </button>
              </Tooltip>
              <Tooltip label={t("sidebar.plugins")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className={`sidebar__navitem${pluginSurface && !searchOpen ? " sidebar__navitem--active" : ""}`}
                  type="button"
                  onClick={() => {
                    setMainSurface("plugins");
                    closeWorkspacePanel();
                    setWorkspacePanelMaximized(false);
                  }}
                >
                  <Plug size={18} />
                  <span>{t("sidebar.plugins")}</span>
                </button>
              </Tooltip>
              <Tooltip label={t("sidebar.automation")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button className="sidebar__navitem" type="button" onClick={() => setSettingsTarget("permissions")}>
                  <Workflow size={18} />
                  <span>{t("sidebar.automation")}</span>
                </button>
              </Tooltip>
            </nav>

            {sidebarPinnedProjects.length > 0 && (
              <section className={`sidebar__section sidebar__section--pinned${collapsedSidebarSections.pinned ? " sidebar__section--collapsed" : ""}`}>
                <button
                  className={`sidebar__section-head${collapsedSidebarSections.pinned ? " sidebar__section-head--collapsed" : ""}`}
                  type="button"
                  aria-expanded={!collapsedSidebarSections.pinned}
                  aria-controls="sidebar-pinned-list"
                  onClick={() => toggleSidebarSection("pinned")}
                >
                  <span>{t("sidebar.pinned")}</span>
                  {collapsedSidebarSections.pinned ? (
                    <ChevronRight className="sidebar__section-caret" size={14} />
                  ) : (
                    <ChevronDown className="sidebar__section-caret" size={14} />
                  )}
                </button>
                {!collapsedSidebarSections.pinned && (
                  <>
                    <div id="sidebar-pinned-list" className="sidebar__pinned-list">
                      {sidebarPinnedProjects.map((project) => (
                        <div
                          key={project.root}
                          className={`sidebar__pinned-project${pinnedProjectMenu?.root === project.root ? " sidebar__pinned-project--menu-open" : ""}`}
                          onContextMenu={(event) => openPinnedProjectMenu(event, project)}
                        >
                          <button
                            className="sidebar__pinned-project-main"
                            type="button"
                            onClick={() => void openProjectHistory("project", project.root)}
                          >
                            <FolderOpen size={18} />
                            <span>{project.label}</span>
                          </button>
                          <button
                            className="sidebar__pinned-action"
                            type="button"
                            aria-label={t("projectTree.projectActions")}
                            onClick={(event) => openPinnedProjectMenu(event, project)}
                          >
                            <MoreHorizontal size={16} />
                          </button>
                          <button
                            className="sidebar__pinned-action"
                            type="button"
                            aria-label={t("projectTree.renameProject")}
                            onClick={() => void renamePinnedProject(project.root, project.label)}
                          >
                            <Pencil size={15} />
                          </button>
                        </div>
                      ))}
                    </div>
                    <ContextMenu
                      open={Boolean(pinnedProjectMenu)}
                      point={pinnedProjectMenuPoint}
                      items={pinnedProjectMenuItems}
                      minWidth={246}
                      ariaLabel={t("projectTree.projectActions")}
                      onClose={closePinnedProjectMenu}
                    />
                  </>
                )}
              </section>
            )}

            <section className={`sidebar__section sidebar__section--projects${collapsedSidebarSections.projects ? " sidebar__section--collapsed" : ""}`}>
              <button
                className={`sidebar__section-head${collapsedSidebarSections.projects ? " sidebar__section-head--collapsed" : ""}`}
                type="button"
                aria-expanded={!collapsedSidebarSections.projects}
                aria-controls="sidebar-project-list"
                onClick={() => toggleSidebarSection("projects")}
              >
                <span>{t("sidebar.projects")}</span>
                {collapsedSidebarSections.projects ? (
                  <ChevronRight className="sidebar__section-caret" size={14} />
                ) : (
                  <ChevronDown className="sidebar__section-caret" size={14} />
                )}
              </button>
              {!collapsedSidebarSections.projects && (
                <div id="sidebar-project-list">
                  <ProjectTree
                    activeScope={activeTab?.scope}
                    activeWorkspaceRoot={activeTab?.workspaceRoot}
                    activeTopicId={activeTab?.topicId}
                    excludeProjectRoots={sidebarPinnedRoots}
                    onOpenTopic={handleOpenTopic}
                    onOpenProjectHistory={openProjectHistory}
                    onTopicsChanged={refreshProjectsAndTabs}
                    onTreeLoaded={setSidebarProjectTree}
                    onRenameTopic={renameTopic}
                    refreshSignal={projectRevision}
                    onAddProject={async () => {
                      await switchFolder();
                    }}
                  />
                </div>
              )}
            </section>

            <section className={`sidebar__recent${collapsedSidebarSections.recent ? " sidebar__section--collapsed" : ""}`} aria-label={t("sidebar.recent")}>
              <button
                className={`sidebar__section-head${collapsedSidebarSections.recent ? " sidebar__section-head--collapsed" : ""}`}
                type="button"
                aria-expanded={!collapsedSidebarSections.recent}
                aria-controls="sidebar-recent-list"
                onClick={() => toggleSidebarSection("recent")}
              >
                <span>{t("sidebar.recent")}</span>
                {collapsedSidebarSections.recent ? (
                  <ChevronRight className="sidebar__section-caret" size={14} />
                ) : (
                  <ChevronDown className="sidebar__section-caret" size={14} />
                )}
              </button>
              {!collapsedSidebarSections.recent && (
                <div id="sidebar-recent-list" className="sidebar__recent-list">
                  {recentSessions.length > 0 ? (
                    recentSessions.map((session) => (
                      <button
                        key={session.path}
                        className={`sidebar-recent${session.current ? " sidebar-recent--current" : ""}${recentSessionMenu?.path === session.path ? " sidebar-recent--menu-open" : ""}`}
                        type="button"
                        onClick={() => void onResumeSession(session)}
                        onContextMenu={(event) => openRecentSessionMenu(event, session)}
                        onKeyDown={(event) => {
                          if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                            openRecentSessionMenu(event, session);
                          }
                        }}
                        disabled={state.running}
                      >
                        <MessageCircle size={14} />
                        <span className="sidebar-recent__title">{sessionSidebarTitle(session)}</span>
                        <span className="sidebar-recent__time">{sidebarAgeLabel(session, locale)}</span>
                      </button>
                    ))
                  ) : (
                    <div className="sidebar__empty">{t("sidebar.recentEmpty")}</div>
                  )}
                  <ContextMenu
                    open={Boolean(recentSessionMenu)}
                    point={recentSessionMenuPoint}
                    items={recentSessionMenuItems}
                    minWidth={244}
                    ariaLabel={t("history.historySessionActions")}
                    onClose={closeRecentSessionMenu}
                  />
                </div>
              )}
            </section>
          </div>
          {sidebarScrollThumb.visible && (
            <div
              className="sidebar__scrollbar"
              aria-hidden="true"
              style={{
                height: `${sidebarScrollThumb.trackHeight}px`,
                transform: `translateY(${sidebarScrollThumb.trackTop}px)`,
              }}
            >
              <div
                className="sidebar__scrollbar-thumb"
                style={{
                  height: `${sidebarScrollThumb.height}px`,
                  transform: `translateY(${sidebarScrollThumb.top}px)`,
                }}
                onPointerDown={beginSidebarScrollThumbDrag}
                onPointerMove={moveSidebarScrollThumb}
                onPointerUp={endSidebarScrollThumbDrag}
                onPointerCancel={endSidebarScrollThumbDrag}
              />
            </div>
          )}
          </div>

          <nav className="sidebar__nav sidebar__nav--bottom">
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
          {!pluginSurface && (
          <header className="workspace-tabs-bar">
            <TabBar
              tabs={visibleTabs}
              activeTabId={visibleTabId}
              revealActiveSignal={tabRevealSignal}
              onTabChange={(id) => void handleTabChange(id)}
              onTabClose={(id) => void handleTabClose(id)}
              onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
              onTabsReorder={(ids) => void handleTabsReorder(ids)}
              onNewTab={() => void handleNewTab()}
            />
            {!workspacePanelMaximized && (
              <Tooltip
                label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                className={[
                  "workspace-dock-toggle",
                  workspacePanelRenderable ? "workspace-dock-toggle--open" : "workspace-dock-toggle--closed",
                ].join(" ")}
              >
                <button
                  className="workspace-dock-toggle__button"
                  type="button"
                  onClick={workspacePanelRenderable ? closeWorkspacePanel : () => openWorkspacePanel("files")}
                  aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                  aria-pressed={workspacePanelRenderable}
                >
                  {workspacePanelRenderable ? <PanelRightClose size={15} /> : <PanelRightOpen size={15} />}
                </button>
              </Tooltip>
            )}
          </header>
          )}

          {pluginSurface ? (
            <Suspense fallback={<div className="surface-loading" />}>
              <PluginHub
                onTry={(prompt) => {
                  setMainSurface("chat");
                  void handleSend(prompt);
                }}
              />
            </Suspense>
          ) : (
          <>
          <header className="topicbar">
            <div className="topicbar__identity">
              <div className="topicbar__title-row">
                {topicbarEditing ? (
                  <div className="topicbar__title-edit">
                    {topicbarProjectPrefix && (
                      <span className="topicbar__title-prefix">{topicbarProjectPrefix} /</span>
                    )}
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
                  <h1>{topicTitle(activeTab)}</h1>
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
            <div className="topicbar__actions topicbar__actions--codex">
              <Tooltip label={t("thread.openEditor")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--editor topicbar__editor-switcher"
                  type="button"
                  onClick={() => openWorkspacePanel("files")}
                  aria-label={t("thread.openEditor")}
                >
                  <Code2 size={17} />
                  <ChevronDown size={12} />
                </button>
              </Tooltip>
              <Tooltip label={t("thread.controls")} disabled={environmentPanelOpen}>
                <button
                  className={`topicbar__action-btn topicbar__action-btn--icon${environmentPanelOpen ? " topicbar__action-btn--active" : ""}`}
                  type="button"
                  onClick={() => setEnvironmentPanelOpen((open) => !open)}
                  aria-label={t("thread.controls")}
                  aria-controls="thread-environment-panel"
                  aria-pressed={environmentPanelOpen}
                >
                  <SlidersHorizontal size={16} />
                </button>
              </Tooltip>
              <Tooltip label={terminalOpen ? t("rightDock.hideTerminal") : t("rightDock.showTerminal")} disabled={terminalOpen}>
                <button
                  className={`topicbar__action-btn topicbar__action-btn--icon${terminalOpen ? " topicbar__action-btn--active" : ""}`}
                  type="button"
                  onClick={() => setTerminalOpen((open) => !open)}
                  aria-label={terminalOpen ? t("rightDock.hideTerminal") : t("rightDock.showTerminal")}
                  aria-pressed={terminalOpen}
                >
                  <PanelBottom size={16} />
                </button>
              </Tooltip>
              <Tooltip label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")} disabled={workspacePanelRenderable}>
                <button
                  className={`topicbar__action-btn topicbar__action-btn--icon${workspacePanelRenderable ? " topicbar__action-btn--active" : ""}`}
                  type="button"
                  onClick={workspacePanelRenderable ? closeWorkspacePanel : () => openWorkspacePanel("launcher")}
                  aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                  aria-pressed={workspacePanelRenderable}
                >
                  <PanelRightOpen size={16} />
                </button>
              </Tooltip>
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner />

          {environmentPanelOpen && chatWorkbenchActive && environmentPanel}

          <main className="main">
            {state.meta?.ready === false && !state.meta?.startupErr ? (
              <div className="loading-screen">
                <div className="loading-screen__spinner" />
                <span className="loading-screen__text">{t("common.loading")}</span>
              </div>
            ) : (
              <Suspense fallback={<div className="surface-loading surface-loading--transcript" />}>
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
              </Suspense>
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
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              goal={state.meta?.goal}
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
              onSetGoal={applyGoal}
              onClearGoal={clearActiveGoal}
              onSwitchModel={switchModel}
              onSetEffort={setEffort}
              insertRequest={composerInsertRequest}
              disabled={state.meta?.ready === false || state.messageAction != null || state.approval != null || state.ask != null}
              decisionPending={state.messageAction != null || state.approval != null || state.ask != null}
              ready={state.meta?.ready === true}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              retry={state.retry}
            />
            <StatusBar
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              jobs={state.jobs}
              running={state.running}
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              cost={state.sessionCost}
              currency={state.sessionCurrency}
              modelLabel={state.meta?.label}
              currentTurnCount={currentTurnCount}
            />
          </footer>
          {terminalOpen && !homeEmpty && (
            <section className="workspace-terminal" aria-label={t("rightDock.terminal")}>
              <div className="workspace-terminal__head">
                <div className="workspace-terminal__tabs">
                  <div className="workspace-terminal__tab">
                    <SquareTerminal size={15} />
                    <span>{terminalTabTitle}</span>
                  </div>
                  <button className="workspace-terminal__new-tab" type="button" aria-label={t("rightDock.terminal")}>
                    <Plus size={16} />
                  </button>
                </div>
                <button className="workspace-terminal__close" type="button" onClick={() => setTerminalOpen(false)} aria-label={t("common.close")}>
                  <X size={15} />
                </button>
              </div>
              <div className="workspace-terminal__body" ref={terminalBodyRef}>
                {terminalOutput ? (
                  <pre className="workspace-terminal__stream">{terminalOutput}</pre>
                ) : (
                  <div className="workspace-terminal__empty">
                    {terminalStarting ? t("common.loading") : terminalExited ? t("rightDock.terminalExited") : t("rightDock.terminalEmpty")}
                  </div>
                )}
                <form className="workspace-terminal__input" onSubmit={submitTerminalCommand}>
                  <span>{terminalPrompt}</span>
                  <input
                    value={terminalDraft}
                    onChange={(event) => setTerminalDraft(event.target.value)}
                    placeholder={t("rightDock.terminalPlaceholder")}
                    spellCheck={false}
                    disabled={terminalStarting || terminalExited}
                  />
                </form>
              </div>
            </section>
          )}
          </>
          )}
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
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__chrome">
              <button
                type="button"
                className="workbench-dock__chrome-btn"
                onClick={() => void handleNewTab()}
                aria-label={t("rightDock.newSideChat")}
              >
                <Plus size={18} />
              </button>
              <div className="workbench-dock__chrome-spacer" />
              <button
                type="button"
                className="workbench-dock__chrome-btn"
                onClick={() => setWorkspacePanelMaximized((value) => !value)}
                aria-label={workspacePanelMaximized ? t("workspace.restore") : t("workspace.maximize")}
                aria-pressed={workspacePanelMaximized}
              >
                <Maximize2 size={16} />
              </button>
              <button
                type="button"
                className={`workbench-dock__chrome-btn${terminalOpen ? " workbench-dock__chrome-btn--active" : ""}`}
                onClick={openBottomTerminal}
                aria-label={t("rightDock.showTerminal")}
                aria-pressed={terminalOpen}
              >
                <PanelBottom size={16} />
              </button>
              <button
                type="button"
                className="workbench-dock__chrome-btn workbench-dock__chrome-btn--active"
                onClick={closeWorkspacePanel}
                aria-label={t("rightDock.collapse")}
              >
                <PanelRightOpen size={16} />
              </button>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "launcher" ? (
                <div className="workbench-launcher" aria-label={t("rightDock.launcher")}>
                  <button type="button" className="workbench-launcher__card" onClick={() => openRightDockMode("files")}>
                    <Folder size={30} />
                    <strong>{t("rightDock.filesTitle")}</strong>
                    <span>{t("rightDock.filesDesc")}</span>
                    <kbd>Ctrl+P</kbd>
                  </button>
                  <button type="button" className="workbench-launcher__card" onClick={() => void handleNewTab()}>
                    <MessageCircle size={30} />
                    <strong>{t("rightDock.sideChatTitle")}</strong>
                    <span>{t("rightDock.sideChatDesc")}</span>
                  </button>
                  <button type="button" className="workbench-launcher__card" onClick={openBrowserFromDock}>
                    <Globe2 size={32} />
                    <strong>{t("rightDock.browserTitle")}</strong>
                    <span>{t("rightDock.browserDesc")}</span>
                    <kbd>Ctrl+T</kbd>
                  </button>
                  <button type="button" className="workbench-launcher__card" onClick={() => openRightDockMode("changed")}>
                    <FileSearch size={31} />
                    <strong>{t("rightDock.reviewTitle")}</strong>
                    <span>{t("rightDock.reviewDesc")}</span>
                    <kbd>Ctrl+Shift+G</kbd>
                  </button>
                  <button type="button" className="workbench-launcher__card" onClick={openBottomTerminal}>
                    <SquareTerminal size={31} />
                    <strong>{t("rightDock.terminalTitle")}</strong>
                    <span>{t("rightDock.terminalDesc")}</span>
                    <kbd>Ctrl+`</kbd>
                  </button>
                </div>
              ) : rightDockMode === "context" ? (
                <Suspense fallback={<div className="surface-loading" />}>
                  <ContextPanel
                    tabId={activeTabId}
                    context={state.context}
                    usage={state.usage}
                    sessionCost={state.sessionCost}
                    sessionCurrency={state.sessionCurrency}
                    scopeLabel={topicScopeLabel(activeTab)}
                    refreshKey={dockRefreshKey}
                  />
                </Suspense>
              ) : (
                <Suspense fallback={<div className="surface-loading" />}>
                  <WorkspacePanel
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
                    initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                    showViewTabs={false}
                  />
                </Suspense>
              )}
            </div>
          </aside>
        )}
      </div>

      {histView !== null && (
        <Suspense fallback={null}>
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
        </Suspense>
      )}

      <Suspense fallback={null}>
        <SearchDialog
          open={searchOpen}
          sessions={searchSessions}
          loading={searchLoading}
          running={state.running}
          onResume={onResumeSession}
          onClose={() => setSearchOpen(false)}
        />
      </Suspense>

      {settingsTarget !== null && (
        <Suspense fallback={<div className="settings-modal-backdrop" />}>
          <SettingsPanel
            initialTab={settingsTarget}
            onClose={() => setSettingsTarget(null)}
            onChanged={() => void refreshMeta()}
          />
        </Suspense>
      )}

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}
    </div>
    </ShellExpandProvider>
  );
}
