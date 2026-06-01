import { useCallback, useMemo, useState } from "react";
import { SquarePen, Brain, History, Settings as SettingsIcon, SlidersHorizontal, Blocks } from "lucide-react";
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
import { parseTodos } from "./lib/tools";
import type { MemoryView, Mode, SessionMeta } from "./lib/types";

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
    rewind,
    setModel,
    fetchMemory,
    remember,
    saveDoc,
  } = useController();
  const t = useT();
  const [mode, setMode] = useState<Mode>("normal");
  const [memView, setMemView] = useState<MemoryView | null>(null);
  const [histView, setHistView] = useState<SessionMeta[] | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [capsOpen, setCapsOpen] = useState(false);
  const [customizeOpen, setCustomizeOpen] = useState(false);

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

  // History drawer: opening fetches the saved-session list; picking one resumes it
  // (the transcript swaps in; the model/folder are unchanged).
  const openHistory = useCallback(async () => {
    setHistView(await listSessions());
  }, [listSessions]);
  const closeHistory = useCallback(() => setHistView(null), []);
  const onResumeSession = useCallback(
    async (path: string) => {
      setHistView(null);
      await resumeSession(path);
    },
    [resumeSession],
  );
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      await deleteSession(path);
      setHistView(await listSessions());
    },
    [deleteSession, listSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      await renameSession(path, title);
      setHistView(await listSessions());
    },
    [renameSession, listSessions],
  );

  // Workspace: open the folder chooser and switch projects (from the status bar's
  // folder button). The hook resets the transcript and refreshes meta on a pick; a
  // cancel is a no-op.
  const switchFolder = useCallback(async () => {
    await pickWorkspace();
  }, [pickWorkspace]);

  const onRemember = useCallback(
    async (scope: string, note: string) => {
      await remember(scope, note);
      setMemView(await fetchMemory());
    },
    [remember, fetchMemory],
  );

  const onSaveDoc = useCallback(
    async (path: string, body: string) => {
      await saveDoc(path, body);
      setMemView(await fetchMemory());
    },
    [saveDoc, fetchMemory],
  );

  return (
    <div className="app">
      <header className="topbar">
        <span className="topbar__model">{state.meta?.label ?? "…"}</span>
        <div className="topbar__spacer" />
        <div className="customize">
          <button
            className={`chip chip--icon ${customizeOpen ? "chip--on" : ""}`}
            onClick={() => setCustomizeOpen((v) => !v)}
            title={t("topbar.customize")}
          >
            <SlidersHorizontal size={13} />
          </button>
          {customizeOpen && (
            <>
              <div className="menu-backdrop" onClick={() => setCustomizeOpen(false)} />
              <div className="menu">
                <button
                  className="menu__item"
                  disabled={state.running}
                  onClick={() => {
                    setCustomizeOpen(false);
                    setSettingsOpen(true);
                  }}
                >
                  <SettingsIcon size={14} />
                  {t("topbar.settings")}
                </button>
                <button
                  className="menu__item"
                  onClick={() => {
                    setCustomizeOpen(false);
                    setCapsOpen(true);
                  }}
                >
                  <Blocks size={14} />
                  {t("caps.title")}
                </button>
                <button
                  className="menu__item"
                  onClick={() => {
                    setCustomizeOpen(false);
                    void openMemory();
                  }}
                >
                  <Brain size={14} />
                  {t("topbar.memory")}
                </button>
              </div>
            </>
          )}
        </div>
        <button
          className="chip chip--icon"
          onClick={() => void openHistory()}
          disabled={state.running}
          title={state.running ? t("common.busyHint") : t("topbar.history")}
        >
          <History size={13} />
        </button>
        <button className="chip chip--icon" onClick={newSession} title={t("topbar.newSession")}>
          <SquarePen size={13} />
        </button>
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
          <Transcript items={state.items} onPrompt={send} onRewind={rewind} />
        )}
      </main>

      <footer className="footer">
        {showTodos && <TodoPanel todos={todos} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
        <Composer
          running={state.running}
          mode={mode}
          onSend={handleSend}
          onCancel={cancel}
          onCycleMode={cycleMode}
          disabled={state.meta?.ready === false}
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
          onPickFolder={() => void switchFolder()}
        />
      </footer>

      {state.approval && (
        <ApprovalModal
          approval={state.approval}
          onAnswer={(allow, session) => {
            // Approving an exit_plan_mode plan leaves plan mode (the controller
            // flips the executor; mirror it here for the indicator).
            if (state.approval!.tool === "exit_plan_mode" && allow) setMode("normal");
            approve(state.approval!.id, allow, session);
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

      {memView !== null && (
        <MemoryPanel
          view={memView}
          onClose={closeMemory}
          onRemember={onRemember}
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
