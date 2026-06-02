import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import {
  SquarePen,
  Brain,
  Blocks,
  History,
  Settings as SettingsIcon,
  MessageSquare,
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
} from "lucide-react";
import logo from "./assets/logo.svg";
import { useT } from "./lib/i18n";
import { useController } from "./lib/useController";
import { Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { StatusBar } from "./components/StatusBar";
import { MemoryPanel } from "./components/MemoryPanel";
import { HistoryPanel } from "./components/HistoryPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import { CapabilitiesPanel } from "./components/CapabilitiesPanel";
import { UpdateBanner } from "./components/UpdateBanner";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { parseTodos } from "./lib/tools";
import { sessionActivityTime } from "./lib/session";
import type { MemoryView, Mode, SessionMeta } from "./lib/types";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";

const SIDEBAR_COLLAPSED_KEY = "reasonix.sidebar.collapsed";
const SIDEBAR_COLLAPSED_WIDTH = 68;
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_MIN_WIDTH = 228;
const SIDEBAR_MAX_WIDTH = 420;
const CHAT_MIN_WIDTH = 420;
const WORKSPACE_PANEL_MIN_WIDTH = 640;
const WORKSPACE_PANEL_DEFAULT_WIDTH = WORKSPACE_PANEL_MIN_WIDTH;
const WORKSPACE_PANEL_MAX_WIDTH = 820;
const WORKSPACE_PANEL_MAX_RATIO = 0.54;
const WORKSPACE_FILE_TREE_PANEL_DEFAULT_WIDTH = 360;
const WORKSPACE_FILE_TREE_PANEL_MIN_WIDTH = 320;
const WORKSPACE_FILE_TREE_PANEL_MAX_WIDTH = 480;
const WORKSPACE_FILE_TREE_PANEL_MAX_RATIO = 0.32;

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampWorkspacePanelWidth(width: number, sidebarWidth = SIDEBAR_DEFAULT_WIDTH, viewportWidth = 1440): number {
  const maxByRatio = Math.floor(viewportWidth * WORKSPACE_PANEL_MAX_RATIO);
  const maxByChat = Math.floor(viewportWidth - sidebarWidth - CHAT_MIN_WIDTH);
  const max = Math.max(WORKSPACE_PANEL_MIN_WIDTH, Math.min(WORKSPACE_PANEL_MAX_WIDTH, maxByRatio, maxByChat));
  return Math.min(max, Math.max(WORKSPACE_PANEL_MIN_WIDTH, Math.round(width)));
}

function clampWorkspaceFileTreePanelWidth(width: number, sidebarWidth = SIDEBAR_DEFAULT_WIDTH, viewportWidth = 1440): number {
  const maxByRatio = Math.floor(viewportWidth * WORKSPACE_FILE_TREE_PANEL_MAX_RATIO);
  const maxByChat = Math.floor(viewportWidth - sidebarWidth - CHAT_MIN_WIDTH);
  const max = Math.max(
    WORKSPACE_FILE_TREE_PANEL_MIN_WIDTH,
    Math.min(WORKSPACE_FILE_TREE_PANEL_MAX_WIDTH, maxByRatio, maxByChat),
  );
  return Math.min(max, Math.max(WORKSPACE_FILE_TREE_PANEL_MIN_WIDTH, Math.round(width)));
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
  return loadLayoutSize("sidebarWidth", SIDEBAR_DEFAULT_WIDTH, clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidth", width, clampSidebarWidth);
}

function loadWorkspacePanelWidth(): number {
  return loadLayoutSize("workspacePanelWidth", WORKSPACE_PANEL_DEFAULT_WIDTH, clampWorkspacePanelWidth);
}

function saveWorkspacePanelWidth(width: number): void {
  saveLayoutSize("workspacePanelWidth", width);
}

function loadWorkspaceFileTreePanelWidth(): number {
  return loadLayoutSize(
    "workspaceFileTreePanelWidth",
    WORKSPACE_FILE_TREE_PANEL_DEFAULT_WIDTH,
    clampWorkspaceFileTreePanelWidth,
  );
}

function saveWorkspaceFileTreePanelWidth(width: number): void {
  saveLayoutSize("workspaceFileTreePanelWidth", width);
}

function sessionTitle(session: SessionMeta, fallback: string): string {
  return session.title || session.preview || fallback;
}

function sessionTime(ms: number): string {
  return new Date(ms).toLocaleDateString([], { month: "short", day: "numeric" });
}

export default function App() {
  const {
    state,
    send,
    cancel,
    approve,
    answerQuestion,
    setPlan,
    setBypass,
    newSession,
    listSessions,
    resumeSession,
    deleteSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    fetchMemory,
    remember,
    forget,
    saveDoc,
  } = useController();
  const t = useT();
  const [mode, setMode] = useState<Mode>("normal");
  const [memView, setMemView] = useState<MemoryView | null>(null);
  const [histView, setHistView] = useState<SessionMeta[] | null>(null);
  const [sidebarSessions, setSidebarSessions] = useState<SessionMeta[]>([]);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(false);
  const [workspacePanelWidth, setWorkspacePanelWidth] = useState(loadWorkspacePanelWidth);
  const [workspaceFileTreePanelWidth, setWorkspaceFileTreePanelWidth] = useState(loadWorkspaceFileTreePanelWidth);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [workspacePreviewModeActive, setWorkspacePreviewModeActive] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [capsOpen, setCapsOpen] = useState(false);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [footerHeight, setFooterHeight] = useState(0);
  const footerRef = useRef<HTMLElement>(null);
  const sidebarBeforeWorkspacePreviewRef = useRef<boolean | null>(null);
  const effectiveSidebarWidth = sidebarCollapsed ? SIDEBAR_COLLAPSED_WIDTH : sidebarWidth;
  const effectiveWorkspacePanelWidth = useMemo(
    () =>
      workspacePreviewModeActive
        ? clampWorkspacePanelWidth(workspacePanelWidth, effectiveSidebarWidth, viewportWidth)
        : clampWorkspaceFileTreePanelWidth(workspaceFileTreePanelWidth, effectiveSidebarWidth, viewportWidth),
    [effectiveSidebarWidth, viewportWidth, workspaceFileTreePanelWidth, workspacePanelWidth, workspacePreviewModeActive],
  );

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve every tool call). normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      setMode(m);
      setPlan(m === "plan");
      setBypass(m === "yolo");
    },
    [setPlan, setBypass],
  );
  // Shift+Tab cycles normal → plan → yolo → normal.
  const cycleMode = useCallback(() => {
    applyMode(mode === "normal" ? "plan" : mode === "plan" ? "yolo" : "normal");
  }, [mode, applyMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      if (mode === "plan") setPlan(true);
      else if (mode === "yolo") setBypass(true);
    },
    [setModel, mode, setPlan, setBypass],
  );

  // The live task list pinned above the composer comes from the most recent
  // top-level todo_write call; it stays visible while work remains, clears itself
  // once every item is completed, and can be dismissed by the user (the ✕). A
  // dismissal is keyed to that list's id, so a fresh todo_write (a new task)
  // brings the panel back.
  const todoItem = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId) return it;
    }
    return null;
  }, [state.items]);
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const showTodos =
    !!todoItem &&
    todoItem.id !== dismissedTodo &&
    todos.length > 0 &&
    todos.some((t) => t.status !== "completed");

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    send(text);
  }, [pendingPlanRevision, send, state.running]);

  // Memory drawer: opening fetches a fresh snapshot; writes re-fetch so the
  // panel reflects what landed on disk.
  const openMemory = useCallback(async () => {
    setMemView(await fetchMemory());
  }, [fetchMemory]);

  const closeMemory = useCallback(() => setMemView(null), []);

  // handleSend intercepts the slash commands that need a desktop-native action
  // before they reach the backend: "/model <ref>" rebuilds on that model, and
  // "/memory" opens the memory drawer. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    (displayText: string, submitText = displayText) => {
      const t = displayText.trim();
      const model = /^\/model\s+(\S+)$/.exec(t);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (t === "/memory") {
        void openMemory();
        return;
      }
      send(t, submitText.trim());
    },
    [switchModel, openMemory, send],
  );

  const refreshSessions = useCallback(async () => {
    const sessions = await listSessions();
    setSidebarSessions(sessions.slice(0, 10));
    return sessions;
  }, [listSessions]);

  useEffect(() => {
    void refreshSessions();
  }, [refreshSessions]);

  useEffect(() => {
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
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
    if (!state.running && state.items.length > 0) void refreshSessions();
  }, [state.running, state.items.length, refreshSessions]);

  const startNewSession = useCallback(async () => {
    await newSession();
    await refreshSessions();
  }, [newSession, refreshSessions]);

  const toggleSidebar = useCallback(() => {
    sidebarBeforeWorkspacePreviewRef.current = null;
    setSidebarCollapsed((collapsed) => {
      const next = !collapsed;
      saveSidebarCollapsed(next);
      return next;
    });
  }, []);

  const handleWorkspacePreviewModeChange = useCallback((active: boolean) => {
    setWorkspacePreviewModeActive(active);
    if (active) {
      if (sidebarBeforeWorkspacePreviewRef.current === null) {
        sidebarBeforeWorkspacePreviewRef.current = sidebarCollapsed;
      }
      if (!sidebarCollapsed) setSidebarCollapsed(true);
      return;
    }
    const restoreCollapsed = sidebarBeforeWorkspacePreviewRef.current;
    sidebarBeforeWorkspacePreviewRef.current = null;
    if (restoreCollapsed !== null && restoreCollapsed !== sidebarCollapsed) {
      setSidebarCollapsed(restoreCollapsed);
    }
  }, [sidebarCollapsed]);

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
      if (workspacePreviewModeActive) {
        const next = clampWorkspacePanelWidth(width, effectiveSidebarWidth, viewportWidth);
        setWorkspacePanelWidth(next);
        saveWorkspacePanelWidth(next);
      } else {
        const next = clampWorkspaceFileTreePanelWidth(width, effectiveSidebarWidth, viewportWidth);
        setWorkspaceFileTreePanelWidth(next);
        saveWorkspaceFileTreePanelWidth(next);
      }
    },
    [effectiveSidebarWidth, viewportWidth, workspacePreviewModeActive],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen || workspacePanelMaximized) return;
      event.preventDefault();
      setWorkspacePanelResizing(true);
      let nextWidth = effectiveWorkspacePanelWidth;
      const clampWidth = workspacePreviewModeActive ? clampWorkspacePanelWidth : clampWorkspaceFileTreePanelWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampWidth(window.innerWidth - moveEvent.clientX, effectiveSidebarWidth, window.innerWidth);
        if (workspacePreviewModeActive) {
          setWorkspacePanelWidth(nextWidth);
        } else {
          setWorkspaceFileTreePanelWidth(nextWidth);
        }
      };
      const onDone = () => {
        if (workspacePreviewModeActive) {
          setWorkspacePanelWidth(nextWidth);
          saveWorkspacePanelWidth(nextWidth);
        } else {
          setWorkspaceFileTreePanelWidth(nextWidth);
          saveWorkspaceFileTreePanelWidth(nextWidth);
        }
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
    [effectiveSidebarWidth, effectiveWorkspacePanelWidth, workspacePanelMaximized, workspacePanelOpen, workspacePreviewModeActive],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(effectiveWorkspacePanelWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePreviewModeActive ? WORKSPACE_PANEL_MIN_WIDTH : WORKSPACE_FILE_TREE_PANEL_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePreviewModeActive ? WORKSPACE_PANEL_MAX_WIDTH : WORKSPACE_FILE_TREE_PANEL_MAX_WIDTH);
      }
    },
    [effectiveWorkspacePanelWidth, setSavedWorkspacePanelWidth, workspacePreviewModeActive],
  );

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--workspace-width": `${effectiveWorkspacePanelWidth}px`,
      }) as CSSProperties,
    [effectiveWorkspacePanelWidth, sidebarWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    setWorkspacePanelOpen(open);
    if (!open) {
      setWorkspacePanelMaximized(false);
      setWorkspacePreviewModeActive(false);
    }
  }, []);

  const toggleWorkspacePanel = useCallback(() => {
    setWorkspacePanelOpen((open) => {
      const next = !open;
      return next;
    });
  }, []);

  // History drawer: opening fetches the saved-session list; picking one resumes it
  // (the transcript swaps in; the model/folder are unchanged).
  const openHistory = useCallback(async () => {
    setHistView(await refreshSessions());
  }, [refreshSessions]);
  const closeHistory = useCallback(() => setHistView(null), []);
  const onResumeSession = useCallback(
    async (path: string) => {
      setHistView(null);
      await resumeSession(path);
      await refreshSessions();
    },
    [resumeSession, refreshSessions],
  );
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      await deleteSession(path);
      setHistView(await refreshSessions());
    },
    [deleteSession, refreshSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      await renameSession(path, title);
      setHistView(await refreshSessions());
    },
    [renameSession, refreshSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick; refresh the sidebar sessions too so
  // the recent list belongs to the newly selected workspace. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) await refreshSessions();
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshSessions]);

  const onRemember = useCallback(
    async (scope: string, note: string) => {
      await remember(scope, note);
      setMemView(await fetchMemory());
    },
    [remember, fetchMemory],
  );

  const onForget = useCallback(
    async (name: string) => {
      await forget(name);
      setMemView(await fetchMemory());
    },
    [forget, fetchMemory],
  );

  const onSaveDoc = useCallback(
    async (path: string, body: string) => {
      await saveDoc(path, body);
      setMemView(await fetchMemory());
    },
    [saveDoc, fetchMemory],
  );

  const sidebarExpandBlocked = sidebarCollapsed && workspacePreviewModeActive;
  const sidebarToggleTitle = sidebarExpandBlocked
    ? t("sidebar.expandBlocked")
    : sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");

  return (
    <div className="app">
      <div
        className={[
          "layout",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelOpen ? "layout--workspace-open" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        <aside className={`sidebar${sidebarCollapsed ? " sidebar--collapsed" : ""}`} aria-label="Reasonix navigation">
          <div className="sidebar__brand">
            <img src={logo} alt="" className="sidebar__logo" />
            <span>Reasonix</span>
            <button
              className={`sidebar__toggle${sidebarExpandBlocked ? " sidebar__toggle--blocked" : ""}`}
              onClick={sidebarExpandBlocked ? undefined : toggleSidebar}
              title={sidebarToggleTitle}
              aria-label={sidebarToggleTitle}
              aria-disabled={sidebarExpandBlocked}
            >
              {sidebarCollapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />}
            </button>
          </div>

          <button
            className="sidebar__new"
            onClick={() => void startNewSession()}
            disabled={state.running}
            title={state.running ? t("common.busyHint") : t("topbar.newSession")}
          >
            <SquarePen size={15} />
            <span>{t("topbar.newSession")}</span>
          </button>

          <section className="sidebar__section">
            <div className="sidebar__section-head">
              <div className="sidebar__section-title">{t("sidebar.conversations")}</div>
              <button
                className="sidebar__view-all"
                onClick={() => void openHistory()}
                disabled={state.running}
                title={state.running ? t("common.busyHint") : t("topbar.history")}
              >
                {t("sidebar.viewAll")}
              </button>
            </div>
            <div className="sidebar__sessions">
              {sidebarSessions.length === 0 ? (
                <div className="sidebar__empty">{t("sidebar.noRecent")}</div>
              ) : (
                sidebarSessions.map((session) => (
                  <button
                    className={`sidebar-session${session.current ? " sidebar-session--current" : ""}`}
                    key={session.path}
                    onClick={() => void onResumeSession(session.path)}
                    disabled={state.running || session.current}
                    title={session.path}
                  >
                    <MessageSquare size={14} />
                    <span className="sidebar-session__body">
                      <span className="sidebar-session__title">{sessionTitle(session, t("history.emptySession"))}</span>
                      <span className="sidebar-session__meta">
                        {session.current ? t("history.current") : sessionTime(sessionActivityTime(session))}
                      </span>
                    </span>
                  </button>
                ))
              )}
            </div>
          </section>

          <nav className="sidebar__nav">
            <button
              className="sidebar__navitem sidebar__navitem--sessions"
              onClick={() => void openHistory()}
              disabled={state.running}
              title={state.running ? t("common.busyHint") : t("topbar.history")}
            >
              <History size={15} />
              <span>{t("topbar.history")}</span>
            </button>
            <button className="sidebar__navitem" onClick={() => void openMemory()} title={t("topbar.memory")}>
              <Brain size={15} />
              <span>{t("topbar.memory")}</span>
            </button>
            <button className="sidebar__navitem" onClick={() => setCapsOpen(true)} title={t("caps.title")}>
              <Blocks size={15} />
              <span>{t("caps.title")}</span>
            </button>
            <button
              className="sidebar__navitem"
              onClick={() => setSettingsOpen(true)}
              disabled={state.running}
              title={state.running ? t("common.busyHint") : t("topbar.settings")}
            >
              <SettingsIcon size={15} />
              <span>{t("topbar.settings")}</span>
            </button>
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
          onDoubleClick={() => setExpandedSidebarWidth(SIDEBAR_DEFAULT_WIDTH)}
          title={t("sidebar.resize")}
        />

        <section className="chat-pane">
          <header className="topbar">
            <div className="topbar__identity">
              <span className="topbar__title">Reasonix</span>
              <span className="topbar__model">{state.meta?.label ?? "…"}</span>
            </div>
            <div className="topbar__spacer" />
            <button
              className="chip chip--icon topbar__workspace-toggle"
              onClick={toggleWorkspacePanel}
              title={workspacePanelOpen ? t("workspace.close") : t("workspace.open")}
            >
              {workspacePanelOpen ? <PanelRightClose size={13} /> : <PanelRightOpen size={13} />}
            </button>
            <div className="topbar__actions">
              <button
                className="chip chip--icon"
                onClick={() => void openHistory()}
                disabled={state.running}
                title={state.running ? t("common.busyHint") : t("topbar.history")}
              >
                <History size={13} />
              </button>
              <button className="chip chip--icon" onClick={() => void openMemory()} title={t("topbar.memory")}>
                <Brain size={13} />
              </button>
              <button className="chip chip--icon" onClick={() => setCapsOpen(true)} title={t("caps.title")}>
                <Blocks size={13} />
              </button>
              <button
                className="chip chip--icon"
                onClick={() => setSettingsOpen(true)}
                disabled={state.running}
                title={state.running ? t("common.busyHint") : t("topbar.settings")}
              >
                <SettingsIcon size={13} />
              </button>
              <button
                className="chip chip--icon"
                onClick={() => void startNewSession()}
                disabled={state.running}
                title={state.running ? t("common.busyHint") : t("topbar.newSession")}
              >
                <SquarePen size={13} />
              </button>
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner />

          <main className="main">
            {state.meta?.ready === false && !state.meta?.startupErr ? (
              <div className="loading-screen">
                <div className="loading-screen__spinner" />
                <span className="loading-screen__text">{t("common.loading")}</span>
              </div>
            ) : (
              <Transcript items={state.items} footerHeight={footerHeight} onPrompt={send} onRewind={rewind} />
            )}
          </main>

          <footer className="footer" ref={footerRef}>
            {showTodos && <TodoPanel todos={todos} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
            {state.approval && (
              <ApprovalModal
                approval={state.approval}
                onAnswer={(allow, session) => {
                  // Approving an exit_plan_mode plan leaves plan mode (the controller
                  // flips the executor; mirror it here for the indicator).
                  if (state.approval!.tool === "exit_plan_mode" && allow) setMode("normal");
                  approve(state.approval!.id, allow, session);
                }}
                onRevisePlan={(text) => {
                  setPendingPlanRevision(text);
                  approve(state.approval!.id, false, false);
                }}
                onExitPlan={() => {
                  setMode("normal");
                  setPlan(false);
                  approve(state.approval!.id, false, false);
                }}
              />
            )}
            <Composer
              running={state.running}
              mode={mode}
              cwd={state.meta?.cwd}
              onSend={handleSend}
              onCancel={cancel}
              onCycleMode={cycleMode}
              onPickFolder={switchFolder}
              disabled={state.meta?.ready === false || state.approval != null}
            />
            <StatusBar
              meta={state.meta}
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              jobs={state.jobs}
              running={state.running}
              mode={mode}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              onSwitchModel={switchModel}
            />
          </footer>
        </section>

        {workspacePanelOpen && !workspacePanelMaximized && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("workspace.resizePanel")}
            aria-valuemin={workspacePreviewModeActive ? WORKSPACE_PANEL_MIN_WIDTH : WORKSPACE_FILE_TREE_PANEL_MIN_WIDTH}
            aria-valuemax={workspacePreviewModeActive ? WORKSPACE_PANEL_MAX_WIDTH : WORKSPACE_FILE_TREE_PANEL_MAX_WIDTH}
            aria-valuenow={effectiveWorkspacePanelWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() =>
              setSavedWorkspacePanelWidth(
                workspacePreviewModeActive ? WORKSPACE_PANEL_DEFAULT_WIDTH : WORKSPACE_FILE_TREE_PANEL_DEFAULT_WIDTH,
              )
            }
            title={t("workspace.resizePanel")}
          />
        )}

        <WorkspacePanel
          open={workspacePanelOpen}
          cwd={state.meta?.cwd}
          maximized={workspacePanelMaximized}
          panelWidth={workspacePanelMaximized ? viewportWidth - effectiveSidebarWidth : effectiveWorkspacePanelWidth}
          onClose={() => setWorkspacePanel(false)}
          onToggleMaximized={() => setWorkspacePanelMaximized((value) => !value)}
          onPreviewModeChange={handleWorkspacePreviewModeChange}
        />
      </div>

      {state.ask && (
        <AskCard
          ask={state.ask}
          onAnswer={answerQuestion}
          onDismiss={() => answerQuestion(state.ask!.id, [])}
        />
      )}

      {memView !== null && (
        <MemoryPanel
          view={memView}
          onClose={closeMemory}
          onRemember={onRemember}
          onForget={onForget}
          onSaveDoc={onSaveDoc}
        />
      )}

      {histView !== null && (
        <HistoryPanel
          sessions={histView}
          onResume={onResumeSession}
          onDelete={onDeleteSession}
          onRename={onRenameSession}
          onClose={closeHistory}
        />
      )}

      {settingsOpen && <SettingsPanel onClose={() => setSettingsOpen(false)} onChanged={() => void refreshMeta()} />}

      {capsOpen && <CapabilitiesPanel onClose={() => setCapsOpen(false)} />}
    </div>
  );
}
