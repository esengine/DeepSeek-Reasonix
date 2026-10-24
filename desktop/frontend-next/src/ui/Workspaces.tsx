import { inScope } from "./sessionScope";
import { Fragment, type ReactNode, memo, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { hidesLegacySessions, onHidesLegacySessionsChange } from "../state/prefs";
import { onSessionQuickActionsChange, showsSessionQuickActions } from "../state/sessionQuick";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { HubPort, RuntimeView, TreeSession, TreeWorkspace } from "../port/hub";
import { workspaceContains } from "../port/hub";
import type { Adder } from "./addws";
import { pinToViewport } from "./place";
import { useRailQuery } from "./railsearch";
import { StudioIcon } from "./StudioIcon";
import { TreeEmpty, TreeReading } from "./TreeReading";
import { RowCount } from "./RowCount";
import { ListingProgress } from "./ListingProgress";
import { Cross } from "./glyphs";
import { copyText } from "./CopyButton";
import { sessionInfoText } from "./sessionInfo";
import { saveSessionFile } from "./sessionExport";
import { useTreeKeys } from "./tree";
import { useDismiss } from "./dismiss";
import { asksDelete } from "./keys";
import { clearDraftForSession } from "./drafts";
import { WorkspaceOrder, workspaceMenuKeys } from "./WorkspaceOrder";
import { WorkspaceReveal } from "./WorkspaceReveal";
import { SessionMenu } from "./SessionMenu";
import { useSessionMove } from "./useSessionMove";
import { useSessionMoveTo } from "./useSessionMoveTo";
import { pinnedFirst } from "./pinorder";
import { Confirm, removeHint, sessionDeleteHint } from "./WorkspaceConfirm";
import { UnreadCount, UnreadDot } from "./UnreadMark";
import { PeerThreadMark } from "./peerthreads";
import { NO_PAGING, atCap, sessionCount, answers, unloaded, type TreePaging, type TreeView } from "./treewindow";
import { SessionBatch, useSelectedRows } from "./SessionBatch";
import { ProjectsHeader } from "./ProjectsHeader";
import { useWorkspaceDrag } from "./useWorkspaceDrag";
import { SessionAge } from "./SessionAge";
import { SessionArchiveAction, SessionPinAction } from "./SessionRowActions";
import { useSessionMutation, type SessionMutation } from "./useSessionMutation";

const parentOf = (root: string) => root.replace(/[/\\]+$/, "").split(/[/\\]/).slice(-2, -1)[0] ?? "";

interface Props {
  hub: HubPort;
  tree: TreeWorkspace[];
  // False until the kernel first answers; an empty tree before then is unread.
  treeRead: boolean;
  answered?: TreeView | null;
  runtimes: RuntimeView[];
  active: string;
  // Collapsed workspaces are the window's own state, not the kernel's, so the
  // sidebar keeps them here rather than asking for them back every reload.
  folded: Set<string>;
  onFold: (root: string, folded: boolean) => void;
  reload: () => Promise<void>;
  retry?: () => void;
  paging?: TreePaging;
  previewLimit?: number;
  onToggleAll?: () => void;
  allFolded?: boolean;
  canFold?: boolean;
  // The session path being opened, or empty. The row shows it selected and busy
  // before the kernel answers; the owner clears it on failure.
  opening?: string;
  onOpen: (req: { root?: string; sessionPath?: string }) => Promise<void>;
  onFocus: (id: string) => void;
  onClose: (ids: string[]) => Promise<void>;
  // Which of these panes are mid-turn. A callback rather than a prop: run state
  // changes constantly and this is only ever asked at confirmation time.
  liveIds: (ids: string[]) => string[];
  // Which of these belong in the running list: the live ones, and the ones that finished within the chosen grace.
  listedIds?: (ids: string[]) => string[];
  runs: Record<string, { run: string; live: boolean }>;
  scope?: "all" | "live" | "pinned" | "archived";
  pinned?: Set<string>;
  onPin?: (path: string) => void;
  onPause?: (runtimeId: string) => void;
  onBranch?: (runtimeId: string, name: string) => Promise<void>;
  onArchive?: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  onRename: (path: string, title: string, mode?: "inline" | "dialog") => void;
  onError: (e: unknown) => void;
  // 打开项目这个动作归 App —— 首启那条横幅按的是同一个它。
  adder: Adder;
  // Remote projects share the same tree and keyboard navigation as local ones.
  children?: ReactNode;
  hasOtherRows?: (needle: string, hideLegacy: boolean) => boolean;
}

// A conversation a pane holds before its first turn is written has only a file
// name; calling it that would show a timestamp where every other row shows words.
const rowLabel = (session: TreeSession) =>
  session.title || (session.runtimeId && !session.turns ? t("新会话") : session.name);

function WorkspacesView({ hub, tree, treeRead, answered = null, runtimes, active, folded, reload, retry, paging = NO_PAGING, previewLimit = Infinity, onFold, onToggleAll, allFolded, canFold, opening = "", onOpen, onFocus, onClose, liveIds, listedIds = liveIds, runs, scope = "all", pinned = new Set(), onPin = () => {}, onPause = () => {}, onBranch, onArchive = async () => {}, onRename, onError, adder, children, hasOtherRows }: Props) {
  const [busy, setBusy] = useState("");
  const [confirm, setConfirm] = useState("");
  const [selection, setSelection] = useState<Set<string> | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  // The row a Delete key asked about. The question replaces that row, so focus
  // goes back to it once the question is gone, or a keyboard is left nowhere.
  const askedByKey = useRef("");
  const needle = useRailQuery();
  useEffect(() => { setSelection((current) => current === null ? null : new Set()); }, [needle, scope]);
  const toggleSelection = (path: string) => setSelection((current) => {
    if (!current) return current;
    const next = new Set(current);
    if (next.has(path)) next.delete(path); else next.add(path);
    return next;
  });
  const treeKeys = useTreeKeys();
  const mutations = useSessionMutation(onError);
  const [editing, setEditing] = useState("");
  const hideLegacy = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  const quickActions = useSyncExternalStore(onSessionQuickActionsChange, showsSessionQuickActions, showsSessionQuickActions);
  const [sessionMenu, setSessionMenu] = useState("");
  const order = useSessionMove(hub, reload, onError, !!needle);
  const moveTo = useSessionMoveTo(hub, tree, reload, rowLabel);
  const [sessionMenuAt, setSessionMenuAt] = useState({ x: 0, y: 0 });
  const sessionMenuBox = useRef<HTMLDivElement>(null);
  const sessionMenuPortal = useRef<HTMLDivElement>(null);
  const menuTrigger = useRef<HTMLElement | null>(null);
  const dismissMenu = () => {
    if (menuTrigger.current?.dataset.target === sessionMenu) menuTrigger.current.focus();
    setSessionMenu("");
  };
  const openRowMenu = (root: string, trigger: HTMLElement, at?: { x: number; y: number }) => {
    menuTrigger.current = trigger;
    const anchor = trigger.getBoundingClientRect();
    setSessionMenuAt(at ?? { x: anchor.right + 8, y: anchor.top - 7 });
    setSessionMenu(root);
  };
  useDismiss(!!sessionMenu, sessionMenuBox, dismissMenu, sessionMenuPortal);
  useEffect(() => {
    if (sessionMenu && menuTrigger.current?.dataset.target === sessionMenu) {
      sessionMenuPortal.current?.querySelector<HTMLButtonElement>('button[role="menuitem"]:not(:disabled)')?.focus();
    }
  }, [sessionMenu]);
  useLayoutEffect(() => {
    if (sessionMenuPortal.current) pinToViewport(sessionMenuPortal.current, sessionMenuAt.x, sessionMenuAt.y, 12);
  }, [sessionMenu, sessionMenuAt]);
  // Finishing is a transition this run witnessed, not a state a row can hold: a
  // persisted done replays nothing when the window opens or the scope changes.
  const priorRun = useRef<Record<string, string>>({});
  const [justDone, setJustDone] = useState<Set<string>>(new Set());

  // A first sighting only records what the state already was, so a row that is
  // done as it mounts is history and says so silently. Each timer clears only
  // the ids it added, which is why nothing has to be cancelled when `runs`
  // moves on: the animation has already been spent by then.
  useEffect(() => {
    const fresh = newlyDone(priorRun.current, runs);
    if (fresh.length === 0) return;
    setJustDone((prev) => new Set([...prev, ...fresh]));
    window.setTimeout(() => {
      setJustDone((prev) => {
        const next = new Set(prev);
        for (const id of fresh) next.delete(id);
        return next;
      });
    }, 1200);
  }, [runs]);
  // Folders whose next page is being read, or could not be.
  const [paged, setPaged] = useState<Record<string, "loading" | "failed">>({});
  const more = async (root: string) => {
    setPaged((prev) => ({ ...prev, [root]: "loading" }));
    try {
      await paging.more(root);
      setPaged(({ [root]: _, ...rest }) => rest);
    } catch {
      setPaged((prev) => ({ ...prev, [root]: "failed" }));
    }
  };
  const [spread, setSpread] = useState<Set<string>>(new Set());
  // Two folders can share a name — a worktree copy carries the project's own.
  // Only then is the extra word worth the room it takes.
  const twice = new Set(tree.map((w) => w.name).filter((n, i, all) => all.indexOf(n) !== i));

  // A row that is already open is a pane to focus, never a second runtime for
  // one transcript — the kernel refuses that, and it is what forked a recovery
  // branch on every save when two writers shared a file.
  const pick = async (ws: TreeWorkspace, session: TreeSession) => {
    // The tree and runtime list arrive from separate reads. During a close or
    // restore the tree can briefly retain an id that no longer exists; focusing
    // that stale id changes no pane and makes the row look unclickable.
    if (session.runtimeId && runtimes.some((rt) => rt.id === session.runtimeId)) {
      onFocus(session.runtimeId);
      return;
    }
    try {
      await onOpen({ root: session.workspaceRoot || ws.root, sessionPath: session.path });
    } catch (e) {
      onError(e);
    }
  };

  const startSession = async (ws: TreeWorkspace) => {
    setBusy("new:" + ws.root);
    try {
      await onOpen({ root: ws.root });
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  // Dragging reorders the whole remembered list, so a search that hides rows turns it off.
  const drag = useWorkspaceDrag(needle ? [] : tree.filter((w) => w.remembered).map((w) => w.root), async (root, before) => {
    try {
      await hub.placeWorkspace(root, before);
      await reload();
    } catch (e) {
      onError(e);
    }
  });

  const panesOf = (root: string) => {
    const workspace = tree.find((ws) => ws.root === root);
    return runtimes.filter((rt) => !rt.host && (workspace ? workspaceContains(workspace, rt.root) : rt.root === root)).map((rt) => rt.id);
  };

  const dropWorkspace = async (ws: TreeWorkspace) => {
    if (confirm !== ws.root) {
      setConfirm(ws.root);
      return;
    }
    setConfirm("");
    try {
      // Closed here for the same reason dropSession does it: the kernel will not
      // pull a folder out from under a pane that is writing, and leaving that to
      // the reader only means asking which of eight tabs belong to this one.
      const open = panesOf(ws.root);
      if (open.length && liveIds(open).length === 0) await onClose(open);
      await hub.removeWorkspace(ws.root);
      await reload();
    } catch (e) {
      onError(e);
    }
  };

  useEffect(() => {
    const path = askedByKey.current;
    if (!path || confirm === path) return;
    askedByKey.current = "";
    // Another question took over: its own button holds focus now.
    if (confirm) return;
    const rows = treeKeys.ref.current?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? [];
    for (const el of rows) if (el.dataset.target === path) el.focus();
  }, [confirm, treeKeys.ref]);

  const askDelete = (path: string) => {
    askedByKey.current = path;
    setConfirm(path);
  };

  const mutate = (session: TreeSession, kind: SessionMutation) => {
    if (sessionMenu) dismissMenu();
    const row = [...(treeKeys.ref.current?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? [])].find((el) => el.dataset.target === session.path) ?? null;
    void mutations.run(session.path, kind, row, () => kind === "read" ? hub.markSessionRead(session.path).then(reload) : kind === "pin" ? onPin(session.path) : onArchive(session.path, !session.archived, session.runtimeId));
  };
  const archive = (session: TreeSession) => mutate(session, "archive");

  const dropSession = async (session: TreeSession, withCopies = false) => {
    if (confirm !== session.path) {
      setConfirm(session.path);
      return;
    }
    // The row is about to go, and focus with it: the next row takes it, or the
    // one before when this was the last.
    const rows = [...(treeKeys.ref.current?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? [])];
    const asked = treeKeys.ref.current?.querySelector('[role="alertdialog"]');
    const after = asked ? rows.findIndex((el) => asked.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING) : -1;
    const heir = after >= 0 ? rows[after] : rows[rows.length - 1];
    setConfirm("");
    try {
      // An idle pane is closed and waited on, since its runtime holds the
      // transcript's lease until it is down. A running one is left for the
      // kernel to refuse: a delete never stops work in progress.
      if (session.runtimeId && liveIds([session.runtimeId]).length === 0) await onClose([session.runtimeId]);
      // A plain delete keeps copies that hold content the conversation lacks; taking
      // them along removes each first, so a refused copy leaves the conversation in place.
      if (withCopies) for (const copy of session.copies ?? []) await hub.removeSession(copy.path);
      await hub.removeSession(session.path);
      clearDraftForSession("", session.path);
      await reload();
      if (document.activeElement && document.activeElement !== document.body) return;
      (heir?.isConnected ? heir : treeKeys.ref.current?.querySelector<HTMLElement>('[role="treeitem"]'))?.focus();
    } catch (e) {
      onError(e);
    }
  };

  const autoName = async (session: TreeSession) => {
    setBusy("name:" + session.path);
    try {
      const { title } = await hub.autoNameSession(session.path);
      await hub.renameSession(session.path, title);
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
      setSessionMenu("");
    }
  };

  const saveSession = async (session: TreeSession, format: "json" | "md" = "json") => {
    setBusy("export:" + session.path);
    try {
      await saveSessionFile(hub, session, format);
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
      setSessionMenu("");
    }
  };

  const hit = (ws: TreeWorkspace) => {
    if (!needle) return ws;
    const named = (x: TreeSession) => (x.title || x.name || "").toLowerCase().includes(needle);
    if (ws.name.toLowerCase().includes(needle)) return ws;
    const sessions = ws.sessions.filter(named);
    return sessions.length || ws.countsComplete === false ? { ...ws, sessions } : null;
  };
  const searchedTree = needle ? (tree.map(hit).filter(Boolean) as TreeWorkspace[]) : tree;
  const shownTree = searchedTree
    .map((ws) => ({
      ...ws,
      sessions: pinnedFirst(ws.sessions.filter((session) =>
        inScope(session, scope, !!needle, !!session.runtimeId && listedIds([session.runtimeId]).length > 0, pinned.has(session.path), hideLegacy)), pinned),
    }))
    .filter((ws) => scope === "all" || ws.sessions.length > 0 || ws.countsComplete === false);
  // A fold is a resting-state preference: while a word is being typed it would
  // hide the very rows that word just found.
  const searching = !!needle;
  const loadedRows = (root: string) => tree.find((x) => x.root === root)?.sessions.length ?? 0;
  // What the kernel is asked to count: the archived rows when that is the view,
  // otherwise the active ones, and everything while a word is being searched
  // for, since a search is allowed to find an archived conversation.
  const kernelScope = scope === "archived" ? "archived" : searching ? "" : "active";
  const [asked, setAsked] = useState(needle);
  useEffect(() => {
    if (!needle) return setAsked("");
    const wait = setTimeout(() => setAsked(needle), 250);
    return () => clearTimeout(wait);
  }, [needle]);
  useEffect(() => {
    paging.view({ scope: kernelScope, q: asked });
  }, [paging, kernelScope, asked]);
  const pagedScope = scope === "all" || scope === "archived";
  const visibleSessions = (ws: TreeWorkspace) => needle || scope !== "all" || selection || expanded.has(ws.root)
    ? ws.sessions : ws.sessions.filter((session, index) => index < previewLimit || session.runtimeId === active || session.path === opening);
  const displayed = shownTree.filter((ws) => needle || !folded.has(ws.root))
    .flatMap(visibleSessions);
  const selected = useSelectedRows(displayed, selection, setSelection);
  const otherRows = hasOtherRows ? hasOtherRows(needle, hideLegacy) : !!children;

  return (
    <>
      {moveTo.dialog}
      <SessionBatch hub={hub} selection={selection} selected={selected} displayed={displayed} onSelect={setSelection}
        onExit={() => treeKeys.ref.current?.previousElementSibling?.querySelector<HTMLElement>('[data-action="machine.menu"]')?.focus()}
        onBusy={(pending) => { setBusy(pending ? "batch" : ""); if (pending) { setSessionMenu(""); setEditing(""); setConfirm(""); } }}
        onClose={onClose} onArchive={onArchive} liveIds={liveIds} reload={reload} onError={onError} />
      <div className="scroll">
          <ProjectsHeader adder={adder}
            onToggleAll={onToggleAll} allFolded={needle ? false : allFolded} canFold={canFold} searching={!!needle}
            canSelect={displayed.length > 0} selecting={selection !== null}
            onSelect={() => setSelection(new Set())} />
        <div role="tree" aria-label={t("项目与会话")} aria-multiselectable={selection !== null} inert={busy === "batch"} data-action-keydown="tree.navigate" ref={treeKeys.ref} onKeyDown={treeKeys.onKeyDown}>
          {shownTree.map((ws) => {
            // A fold is a resting-state preference; while a query is on it would hide
            // the very rows the query just found.
            const hasChildren = ws.sessions.length > 0 || ws.countsComplete === false || ws.listingState === "degraded" || (pagedScope && unloaded(ws, loadedRows(ws.root)) > 0);
            const visible = visibleSessions(ws);
            const hidden = ws.sessions.length - visible.length;
            const shut = hasChildren && !needle && folded.has(ws.root);
            // Only while the question is on screen: panesOf walks every runtime.
            const doomed = confirm === ws.root ? panesOf(ws.root) : [];
            const busyPanes = liveIds(doomed).length;
            return (
              <div className="wsnode" key={ws.root} data-current={panesOf(ws.root).includes(active) ? "" : undefined} data-missing={ws.missing ? "" : undefined}>
                {confirm === ws.root ? (
                  <Confirm
                    what={t("从列表移除「{name}」？", { name: ws.name })}
                    hint={removeHint(doomed.length, busyPanes)}
                    go={t("移除")}
                    danger={busyPanes > 0}
                    onGo={() => void dropWorkspace(ws)}
                    onCancel={() => setConfirm("")}
                  />
                ) : (
                  <div
                    ref={sessionMenu === ws.root ? sessionMenuBox : undefined}
                    data-action-contextmenu="workspace.menu"
                    data-action-keydown="workspace.menu"
                    data-action-dragstart="workspace.reorder"
                    data-action-dragover="workspace.reorder"
                    data-action-dragleave="workspace.reorder"
                    data-action-drop="workspace.reorder"
                    data-action-dragend="workspace.reorder"
                    data-target={ws.root}
                    data-drop={drag.over?.root === ws.root ? drag.over.edge : undefined}
                    data-dragging={drag.dragging === ws.root ? "" : undefined}
                    draggable={ws.remembered && !needle}
                    onDragStart={(ev) => drag.start(ev, ws.root)}
                    onDragOver={(ev) => drag.hover(ev, ws.root)}
                    onDragLeave={() => drag.leave(ws.root)}
                    onDrop={(ev) => drag.drop(ev, ws.root)}
                    onDragEnd={drag.end}
                    className="wsrow"
                    role="treeitem"
                    aria-level={1}
                    aria-haspopup="menu"
                    aria-expanded={hasChildren ? !shut : undefined}
                    onClick={() => { if (hasChildren) onFold(ws.root, !shut); }}
                    onContextMenu={(ev) => {
                      if ((ev.target as HTMLElement).closest("input, textarea, [role='menu']")) return;
                      ev.preventDefault();
                      ev.stopPropagation();
                      openRowMenu(ws.root, ev.currentTarget, ev.clientX || ev.clientY ? { x: ev.clientX, y: ev.clientY } : undefined);
                    }}
                    onKeyDown={(ev) => {
                      if (ev.target !== ev.currentTarget) return;
                      if (ev.key !== "ContextMenu" && !(ev.shiftKey && ev.key === "F10")) return;
                      ev.preventDefault();
                      ev.stopPropagation();
                      openRowMenu(ws.root, ev.currentTarget);
                    }}
                  >
                    <StudioIcon name="folder" className="wsfolder" />
                    <span className="wsname" title={ws.root}>
                      {ws.name}
                    </span>
                    {/* A tag is this project's identity, so it takes a second line and
                        the name keeps the row; the count and the actions share one slot. */}
                    {(ws.isolated || twice.has(ws.name)) && (
                      <span className="wsmeta">
                        <em className="wstag">{ws.isolated ? t("隔离") : parentOf(ws.root)}</em>
                      </span>
                    )}
                    <span className="wsacts">
                      {shut && <UnreadCount n={ws.sessions.filter((x) => x.unread).length} />}
                      <span className="rowswap">
                      {ws.countsComplete !== false && <RowCount c={pagedScope ? sessionCount(ws, ws.sessions.length) : { shown: ws.sessions.length }} />}
                      <span className="rowbtns">
                      <button
                        data-action="session.new"
                        className="wsadd"
                        data-busy={busy === "new:" + ws.root ? "" : undefined}
                        disabled={ws.missing}
                        title={t("在 {name} 下新建会话", { name: ws.name })}
                        aria-label={t("在 {name} 下新建会话", { name: ws.name })}
                        onClick={(ev) => {
                          ev.stopPropagation();
                          void startSession(ws);
                        }}
                      >
                        <StudioIcon name="chat" />
                      </button>
                      <button
                        className="wsmore"
                        data-action="workspace.menu"
                        data-target={ws.root}
                        title={t("更多操作")}
                        aria-label={t("项目操作：{name}", { name: ws.name })}
                        aria-expanded={sessionMenu === ws.root}
                        aria-haspopup="menu"
                        onClick={(ev) => {
                          ev.stopPropagation();
                          if (sessionMenu === ws.root) setSessionMenu("");
                          else openRowMenu(ws.root, ev.currentTarget);
                        }}
                      >
                        <StudioIcon name="more" />
                      </button>
                      </span>
                      </span>
                    </span>
                    {sessionMenu === ws.root && createPortal(
                      <div ref={sessionMenuPortal} className="session-pop" role="menu" aria-label={t("项目操作")} data-action-keydown="workspace.menu" data-target={ws.root} onKeyDown={workspaceMenuKeys} onClick={(ev) => ev.stopPropagation()}>
                        <div className="session-pop-head">
                          <b>{ws.name}</b>
                          <small className="session-pop-path" title={ws.root}>{ws.root}</small>
                        </div>
                        <WorkspaceReveal ws={ws} hub={hub} dismiss={dismissMenu} onError={onError} />
                        {ws.remembered && <WorkspaceOrder root={ws.root} position={tree.filter((w) => w.remembered).findIndex((w) => w.root === ws.root)} total={tree.filter((w) => w.remembered).length}
                          hub={hub} reload={reload} dismiss={dismissMenu} onError={onError} />}
                        <div className="session-pop-group">
                          <button className="danger" role="menuitem" data-action="workspace.remove" onClick={() => { setConfirm(ws.root); dismissMenu(); }}>
                            <StudioIcon name="close" /><span>{t("从列表移除")}</span><small>{t("不删除文件")}</small>
                          </button>
                        </div>
                      </div>,
                      document.body,
                    )}
                  </div>
                )}

                {/* 子项自己成一块，才能拿到那条层级引导线。缩进单独说不清楚层级：
                    两级差 18px，扫一眼只读得出“有点错位”。 */}
                {!shut && hasChildren && (
                  <div className="kids">
                {visible.map((session) => {
                    const on = opening ? session.path === opening : session.runtimeId === active;
                    const run = session.runtimeId ? runs[session.runtimeId]?.run : undefined;
                    if (confirm === session.path) {
                      return (
                        <Confirm
                          key={session.path}
                          what={t("删除「{name}」？", { name: rowLabel(session) })}
                          hint={sessionDeleteHint(session.copies?.length ?? 0, !!session.runtimeId, !!session.runtimeId && liveIds([session.runtimeId]).length > 0)}
                          go={t("删除")}
                          danger
                          alt={(session.copies?.length ?? 0) > 0 ? { label: t("连同 {n} 份副本删除", { n: session.copies!.length }), onGo: () => void dropSession(session, true) } : undefined}
                          onGo={() => void dropSession(session)}
                          onCancel={() => setConfirm("")}
                        />
                      );
                    }
                    const versions = session.versions ?? [];
                    const copies = session.copies ?? [];
                    const kept = [...versions.map((s) => ({ s, version: true })), ...copies.map((s) => ({ s, version: false }))];
                    const open = spread.has(session.path);
                    return (
                      <Fragment key={session.path}>
                      <div
                        ref={sessionMenu === session.path ? sessionMenuBox : undefined}
                        data-action-click={selection ? "session.selection" : "session.open"}
                        data-action-contextmenu="session.menu"
                        aria-haspopup="menu"
                        data-action-keydown={selection ? ["session.selection"] : ["session.open", "session.delete", "session.menu", "session.move"]}
                        data-target={session.path}
                        className="sessrow sessrow-context"
                        role="treeitem"
                        aria-level={2}
                        aria-label={rowLabel(session)}
                        aria-selected={selection ? selection.has(session.path) : on}
                        data-on={on ? "" : undefined}
                        data-live={session.runtimeId ? "" : undefined}
                        data-run={run === "idle" ? undefined : run}
                        data-unread={session.unread ? "" : undefined}
                        data-just-done={session.runtimeId && justDone.has(session.runtimeId) ? "" : undefined}
                        data-busy={opening === session.path || mutations.pending.has(session.path) ? "" : undefined}
                        onClick={(ev) => { if (selection) toggleSelection(session.path); else if (ev.detail < 2 && mutations.pending.get(session.path) !== "archive") void pick(ws, session); }}
                        data-action-dblclick={selection ? undefined : "session.rename"}
                        onDoubleClick={(ev) => {
                          if (selection || mutations.pending.has(session.path) || (ev.target as HTMLElement).closest("input, button, [role='menu']")) return;
                          setEditing(session.path);
                        }}
                        onContextMenu={(ev) => {
                          if ((ev.target as HTMLElement).closest("input, textarea, [role='menu']")) return;
                          if (selection || mutations.pending.has(session.path)) { ev.preventDefault(); return; }
                          ev.preventDefault();
                          ev.stopPropagation();
                          openRowMenu(session.path, ev.currentTarget, { x: ev.clientX, y: ev.clientY });
                        }}
                        tabIndex={0}
                        onKeyDown={(ev) => {
                          if (ev.target !== ev.currentTarget) return;
                          if (mutations.pending.has(session.path)) return;
                          if (editing === session.path) return;
                          if (selection) {
                            if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); ev.stopPropagation(); toggleSelection(session.path); }
                            return;
                          }
                          if (ev.key === "ContextMenu" || ev.shiftKey && ev.key === "F10") {
                            ev.preventDefault(); ev.stopPropagation(); openRowMenu(session.path, ev.currentTarget);
                          } else if (ev.altKey && (ev.key === "ArrowUp" || ev.key === "ArrowDown")) {
                            ev.preventDefault(); ev.stopPropagation();
                            const direction = ev.key === "ArrowUp" ? -1 : 1;
                            if (direction === -1 ? order.canMove(ws, session).up : order.canMove(ws, session).down) void order.move(session, direction);
                          } else if (ev.key === "Enter" || ev.key === " ") {
                            ev.preventDefault();
                            ev.stopPropagation();
                            void pick(ws, session);
                          } else if (asksDelete(ev)) {
                            ev.preventDefault();
                            askDelete(session.path);
                          }
                        }}
                      >
                        {selection && <input type="checkbox" className="session-select" tabIndex={-1} aria-label={t("选择会话：{name}", { name: rowLabel(session) })}
                          data-action-click="session.selection" data-action-change="session.selection" data-target={session.path} checked={selection.has(session.path)}
                          onClick={(ev) => ev.stopPropagation()} onChange={() => toggleSelection(session.path)} />}
                        <i className="pip" />
                        {!selection && <SessionPinAction target={session.path} title={rowLabel(session)}
                          pinned={pinned.has(session.path)} enabled={quickActions} disabled={mutations.pending.has(session.path)} onPin={() => mutate(session, "pin")} />}
                        {editing === session.path ? (
                          <input className="sessedit" aria-label={t("重命名该会话")} placeholder={t("聊天标题")} autoFocus
                            defaultValue={session.title || session.name}
                            onClick={(ev) => ev.stopPropagation()}
                            data-action-blur="session.rename" data-action-keydown="session.rename" data-target={session.path}
                            onBlur={(ev) => {
                              const next = ev.currentTarget.value.trim();
                              setEditing("");
                              if (next !== "" && next !== (session.title || session.name).trim()) onRename(session.path, next, "inline");
                            }}
                            onKeyDown={(ev) => {
                              if (ev.key === "Enter") {
                                ev.preventDefault();
                                ev.currentTarget.blur();
                              } else if (ev.key === "Escape") {
                                ev.preventDefault();
                                ev.stopPropagation();
                                ev.currentTarget.value = session.title || session.name;
                                ev.currentTarget.blur();
                              }
                            }} />
                        ) : (
                          <span className="sesstitle" title={rowLabel(session)}>
                            <span>{rowLabel(session)}</span>{session.isolated && <em className="sessarch" title={session.workspaceRoot}>{t("隔离")}</em>}{session.archived && <em className="sessarch">{t("已归档")}</em>}
                            {session.metadataState === "pending" && <em className="sessarch">{t("正在读取摘要")}</em>}
                            {session.metadataState === "error" && <em className="sessarch">{t("摘要暂不可用")}</em>}
                          </span>
                        )}
                        <PeerThreadMark path={session.path} />
                        {session.unread && <UnreadDot />}
                        <SessionAge at={session.lastActiveAt} />
                        {kept.length > 0 && (
                          <button
                            className="sesscopies"
                            aria-expanded={open}
                            title={
                              versions.length && copies.length
                                ? t("早先版本与恢复副本")
                                : versions.length
                                  ? t("编辑、重新生成或回退之前的对话版本")
                                  : t("本次对话被外部程序改写时留下的副本")
                            }
                            onClick={(ev) => {
                              ev.stopPropagation();
                              setSpread((prev) => {
                                const next = new Set(prev);
                                if (open) next.delete(session.path);
                                else next.add(session.path);
                                return next;
                              });
                            }}
                          >
                            {`+${kept.length}`}
                          </button>
                        )}
                        {session.legacy && (
                          <span className="sesslegacy" title={t("由 Reasonix 1.x 创建：Studio 只读打开，继续对话会另存为新会话")} aria-label={t("由 Reasonix 1.x 创建：Studio 只读打开，继续对话会另存为新会话")}>{t("旧")}</span>
                        )}
                        {quickActions && !selection && <SessionArchiveAction target={session.path} title={rowLabel(session)} archived={!!session.archived}
                          disabled={mutations.pending.has(session.path) || !!session.runtimeId && liveIds([session.runtimeId]).length > 0} onArchive={() => archive(session)} />}
                        {sessionMenu === session.path && (
                          <SessionMenu
                            ref={sessionMenuPortal}
                            session={session}
                            label={rowLabel(session)}
                            live={!!session.runtimeId && liveIds([session.runtimeId]).length > 0}
                            pinned={pinned.has(session.path)}
                            exporting={busy === "export:" + session.path}
                            autoNaming={busy === "name:" + session.path}
                            onPin={() => mutate(session, "pin")}
                            onMarkRead={session.unread ? () => mutate(session, "read") : undefined}
                            onRename={() => { setSessionMenu(""); onRename(session.path, session.title || session.name); }}
                            onAutoName={() => void autoName(session)}
                            onPause={() => { onPause(session.runtimeId!); setSessionMenu(""); }}
                            onClose={() => {
                              const id = session.runtimeId!;
                              setSessionMenu("");
                              void onClose([id]).catch(onError);
                            }}
                            onArchive={() => archive(session)}
                            onBranch={onBranch && session.runtimeId ? () => { setSessionMenu(""); void onBranch(session.runtimeId!, `${rowLabel(session)} · ${t("分支")}`).catch(onError); } : undefined}
                            onCopyInfo={() => { void copyText(sessionInfoText(session, session.workspaceRoot || ws.root)).catch(onError); setSessionMenu(""); }}
                            onExport={(format) => void saveSession(session, format)}
                            onDelete={() => { askDelete(session.path); setSessionMenu(""); }}
                            onMove={(direction) => { setSessionMenu(""); void order.move(session, direction); }}
                            canMove={order.canMove(ws, session)}
                            onMoveTo={() => { setSessionMenu(""); moveTo.request(session, ws.root); }}
                          />
                        )}
                      </div>
                      {open &&
                        kept.map(({ s: copy, version }) =>
                          confirm === copy.path ? (
                            <Confirm
                              key={copy.path}
                              what={version ? t("删除这个早先版本？") : t("删除这份恢复副本？")}
                              hint={t("连同其记录一并删除")}
                              go={t("删除")}
                              danger
                              onGo={() => void dropSession(copy)}
                              onCancel={() => setConfirm("")}
                            />
                          ) : (
                            <div
                              data-action-click="session.open"
                              data-action-keydown={["session.open", "session.delete"]}
                              data-target={copy.path}
                              key={copy.path}
                              className="sessrow sesscopy"
                              role="treeitem"
                              aria-level={3}
                              aria-selected={opening === copy.path}
                              data-on={opening === copy.path ? "" : undefined}
                              data-busy={opening === copy.path ? "" : undefined}
                              onClick={() => void pick(ws, copy)}
                              tabIndex={0}
                              onKeyDown={(ev) => {
                                if (ev.target !== ev.currentTarget) return;
                                if (ev.key === "Enter" || ev.key === " ") {
                                  ev.preventDefault();
                                  void pick(ws, copy);
                                } else if (asksDelete(ev)) {
                                  ev.preventDefault();
                                  askDelete(copy.path);
                                }
                              }}
                            >
                              <i className="pip" />
                              <span className="sesstitle" title={rowLabel(copy)}>
                                <span>{version ? t("早先版本 · {n} 轮", { n: copy.turns ?? 0 }) : t("恢复副本")}</span>
                              </span>
                              <button
                                className="wsdel"
                                data-action="session.delete"
                                data-target={copy.path}
                                title={t("删除该会话")}
                                aria-label={t("删除该会话")}
                                onClick={(ev) => {
                                  ev.stopPropagation();
                                  setConfirm(copy.path);
                                }}
                              >
                                <Cross />
                              </button>
                            </div>
                          ),
                        )}
                      </Fragment>
                    );
                  })}

                <ListingProgress workspace={ws} searching={searching} onRetry={retry} />
                {pagedScope && (hidden > 0 || unloaded(ws, loadedRows(ws.root)) > 0) && (
                  hidden === 0 && atCap(loadedRows(ws.root)) ? (
                    <small className="sessnote">{ws.countsComplete === false ? t("用搜索查找更多会话") : t("还有 {n} 个较早的会话 · 用搜索查找", { n: unloaded(ws, loadedRows(ws.root)) })}</small>
                  ) : (
                    <button
                      className="sessmore"
                      disabled={paged[ws.root] === "loading"}
                      data-action="session.more"
                      onClick={() => {
                        setExpanded((current) => new Set(current).add(ws.root));
                        if (unloaded(ws, loadedRows(ws.root)) > 0 && !atCap(loadedRows(ws.root))) void more(ws.root);
                      }}
                    >
                      {paged[ws.root] === "loading"
                        ? t("正在加载…")
                        : paged[ws.root] === "failed"
                          ? t("加载失败 · 重试")
                          : ws.countsComplete === false ? t("显示更多") : t("显示更多（还有 {n} 个）", { n: hidden + unloaded(ws, loadedRows(ws.root)) })}
                    </button>
                  )
                )}
                  </div>
                )}
              </div>
            );
          })}
          {!otherRows && shownTree.length === 0 && (tree.length > 0 || treeRead && (!!needle || scope !== "all")) && <TreeEmpty settled={tree.every((ws) => ws.countsComplete !== false) && answers(answered, { scope: kernelScope, q: asked })} onRetry={retry} />}
          {tree.length === 0 && (!treeRead || !otherRows && !needle && scope === "all") && (treeRead ? (
            <div className="ws-empty">{t("尚无文件夹")}<button className="projects-open" data-action="workspace.add"
              disabled={adder.busy} onClick={() => adder.add()}><StudioIcon name="folder" />{t("打开第一个项目")}</button></div>
          ) : (
            <TreeReading onRetry={retry} />
          ))}
          {children}
        </div>
      </div>

    </>
  );
}

// Panes report upward on every usage round, so the window repaints often; the
// tree it holds does not change nearly that often.
export const Workspaces = memo(WorkspacesView);

// Which runs reached "done" since `before` last recorded them. A state is not
// an event: `done` persists, so only a change no first sighting can produce —
// both sides known and different — is the completion this window watched.
export function newlyDone(
  before: Record<string, string>,
  runs: Record<string, { run: string; live: boolean }>,
): string[] {
  const fresh: string[] = [];
  for (const [id, st] of Object.entries(runs)) {
    if (st.run === "done" && before[id] !== undefined && before[id] !== "done") fresh.push(id);
    before[id] = st.run;
  }
  return fresh;
}
