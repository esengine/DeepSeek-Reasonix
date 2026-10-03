import { Fragment, type ReactNode, memo, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { seconds } from "../i18n/format";
import type { HubPort, RuntimeView, TreeSession, TreeWorkspace } from "../port/hub";
import type { Adder } from "./addws";
import { pinToViewport } from "./place";
import { useRailQuery } from "./railsearch";
import { StudioIcon } from "./StudioIcon";
import { Cross } from "./glyphs";
import { download } from "../port/download";
import { host } from "../port/host";
import { useTreeKeys } from "./tree";
import { useDismiss } from "./dismiss";
import { asksDelete } from "./keys";
import { clearDraftForSession } from "./drafts";
import { WorkspaceOrder, workspaceMenuKeys } from "./WorkspaceOrder";
import { WorkspaceReveal } from "./WorkspaceReveal";

const parentOf = (root: string) => root.replace(/[/\\]+$/, "").split(/[/\\]/).slice(-2, -1)[0] ?? "";

interface Props {
  hub: HubPort;
  tree: TreeWorkspace[];
  // False until the kernel first answers; an empty tree before then is unread.
  treeRead: boolean;
  runtimes: RuntimeView[];
  active: string;
  // Collapsed workspaces are the window's own state, not the kernel's, so the
  // sidebar keeps them here rather than asking for them back every reload.
  folded: Set<string>;
  onFold: (root: string, folded: boolean) => void;
  reload: () => Promise<void>;
  onOpen: (req: { root?: string; sessionPath?: string }) => Promise<void>;
  onFocus: (id: string) => void;
  onClose: (ids: string[]) => Promise<void>;
  // Which of these panes are mid-turn. A callback rather than a prop: run state
  // changes constantly and this is only ever asked at confirmation time.
  liveIds: (ids: string[]) => string[];
  runs: Record<string, { run: string; live: boolean }>;
  scope?: "all" | "live" | "pinned" | "archived";
  pinned?: Set<string>;
  onPin?: (path: string) => void;
  onPause?: (runtimeId: string) => void;
  onArchive?: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  onRename: (path: string, title: string, mode?: "inline" | "dialog") => void;
  onError: (e: unknown) => void;
  // 打开项目这个动作归 App —— 首启那条横幅按的是同一个它。
  adder: Adder;
  // Remote hosts render into this same tree. The rail is a list of machines and
  // this one is among them: opening a session in a folder on a machine is one
  // intent, and it used to be drawn as two.
  children?: ReactNode;
}

// How many of a folder's sessions get a row before the rest are summarised.
// The list is newest-first, so this is the recent end. A machine that has been
// worked on for months holds thousands of these, and drawing them all put 98k
// nodes in the sidebar — more than the transcript at 20000 turns.
const SHOWN = 5;

// A conversation a pane holds before its first turn is written has only a file
// name; calling it that would show a timestamp where every other row shows words.
const rowLabel = (session: TreeSession) =>
  session.title || (session.runtimeId && !session.turns ? t("新会话") : session.name);

function WorkspacesView({ hub, tree, treeRead, runtimes, active, folded, reload, onFold, onOpen, onFocus, onClose, liveIds, runs, scope = "all", pinned = new Set(), onPin = () => {}, onPause = () => {}, onArchive = async () => {}, onRename, onError, adder, children }: Props) {
  const [busy, setBusy] = useState("");
  // Folding a machine is the reader's own preference, held the way a host row
  // holds it.
  const [hereShut, setHereShut] = useState(false);
  const [confirm, setConfirm] = useState("");
  // The row a Delete key asked about. The question replaces that row, so focus
  // goes back to it once the question is gone, or a keyboard is left nowhere.
  const askedByKey = useRef("");
  const needle = useRailQuery();
  const treeKeys = useTreeKeys();
  const [editing, setEditing] = useState("");
  const [sessionMenu, setSessionMenu] = useState("");
  const [sessionMenuAt, setSessionMenuAt] = useState({ x: 0, y: 0 });
  const sessionMenuBox = useRef<HTMLDivElement>(null);
  const sessionMenuPortal = useRef<HTMLDivElement>(null);
  const menuTrigger = useRef<HTMLElement | null>(null);
  const dismissMenu = () => {
    if (menuTrigger.current?.dataset.target === sessionMenu) menuTrigger.current.focus();
    setSessionMenu("");
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
  const [shownCounts, setShownCounts] = useState<Record<string, number>>({});
  // Conversations whose conflict copies the reader asked to see.
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
    setBusy(session.path);
    try {
      await onOpen({ root: ws.root, sessionPath: session.path });
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
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

  const panesOf = (root: string) => runtimes.filter((rt) => rt.root === root).map((rt) => rt.id);

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

  const dropSession = async (session: TreeSession) => {
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
      await hub.removeSession(session.path);
      clearDraftForSession("", session.path);
      await reload();
      if (document.activeElement && document.activeElement !== document.body) return;
      (heir?.isConnected ? heir : treeKeys.ref.current?.querySelector<HTMLElement>('[role="treeitem"]'))?.focus();
    } catch (e) {
      onError(e);
    }
  };

  const saveSession = async (session: TreeSession) => {
    setBusy("export:" + session.path);
    try {
      const exported = await hub.exportSession(session.path);
      const records = exported.content
        .split(/\r?\n/)
        .filter((line) => line.trim())
        .map((line) => JSON.parse(line) as unknown);
      const content = JSON.stringify(
        { version: 1, title: session.title || session.name, exportedAt: new Date().toISOString(), records },
        null,
        2,
      );
      const clean = (session.title || exported.name || session.name).replace(/[<>:"/\\|?*\x00-\x1f]/g, "-").trim() || "reasonix-session";
      const filename = `${clean}.json`;
      const saved = await host().saveText(filename, content);
      if (saved === null) download(filename, content);
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
      setSessionMenu("");
    }
  };

  // Filtering is client-side because the tree is already here: the kernel has no
  // search route, and a round trip to re-derive what the window is holding would
  // be slower than the typing. A hit on the folder keeps all of its sessions.
  const hit = (ws: TreeWorkspace) => {
    if (!needle) return ws;
    const named = (x: TreeSession) => (x.title || x.name || "").toLowerCase().includes(needle);
    if (ws.name.toLowerCase().includes(needle)) return ws;
    const sessions = ws.sessions.filter(named);
    return sessions.length ? { ...ws, sessions } : null;
  };
  const searchedTree = needle ? (tree.map(hit).filter(Boolean) as TreeWorkspace[]) : tree;
  const shownTree = searchedTree
    .map((ws) => ({
      ...ws,
      sessions: ws.sessions.filter((session) => {
        if (scope === "archived") return !!session.archived;
        if (session.archived) return false;
        if (scope === "live") return !!session.runtimeId && liveIds([session.runtimeId]).length > 0;
        if (scope === "pinned") return pinned.has(session.path);
        return true;
      }),
    }))
    .filter((ws) => scope === "all" || ws.sessions.length > 0);
  // A fold is a resting-state preference: while a word is being typed it would
  // hide the very rows that word just found.
  const shutHere = needle ? false : hereShut;

  return (
    <>
      <div className="scroll">
        <div role="tree" aria-label={t("机器、工作区与会话")} data-action-keydown="tree.navigate" ref={treeKeys.ref} onKeyDown={treeKeys.onKeyDown}>
          {/* This machine is the first row of the list rather than another kind of
              thing, and its add button sits where a host's does: open a folder
              on this machine. */}
          <div
            className="machrow"
            data-here=""
            role="treeitem"
            aria-expanded={!shutHere}
            onClick={() => setHereShut((v) => !v)}
          >
            <button className="twist" tabIndex={-1} aria-hidden="true">
              <svg viewBox="0 0 10 10">
                <path d="M3.4 1.6 6.8 5 3.4 8.4" />
              </svg>
            </button>
            <i className="rmtpip" aria-hidden="true" />
            <span className="rmtname">{t("这台机器")}</span>
            {treeRead && <span className="rmtsub">{t("{n} 个项目", { n: tree.length })}</span>}
            <button
              className="machpick"
              data-busy={adder.busy ? "" : undefined}
              title={t("打开或新建项目…")}
              aria-label={t("打开或新建项目…")}
              data-action="workspace.add"
              onClick={(ev) => {
                ev.stopPropagation();
                adder.add();
              }}
            >
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <path d="M8 3.7v8.6M3.7 8h8.6" />
              </svg>
            </button>
          </div>
          {shutHere ? null : shownTree.map((ws) => {
            // A fold is a resting-state preference; while a query is on it would hide
            // the very rows the query just found.
            const shut = needle ? false : folded.has(ws.root);
            const limit = shownCounts[ws.root] ?? SHOWN;
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
                    className="wsrow"
                    role="treeitem"
                    aria-expanded={!shut}
                    onClick={() => {
                      if (!shut) {
                        setShownCounts((prev) => {
                          const next = { ...prev };
                          delete next[ws.root];
                          return next;
                        });
                      }
                      onFold(ws.root, !shut);
                    }}
                  >
                    <button className="twist" tabIndex={-1} aria-hidden="true">
                      <svg viewBox="0 0 10 10">
                        <path d="M3.4 1.6 6.8 5 3.4 8.4" />
                      </svg>
                    </button>
                    <i className="wsdot" aria-hidden="true" />
                    <span className="wsname" title={ws.root}>
                      {ws.name}
                    </span>
                    {/* 第二行是这个项目的身份，不是它的动作。名字于是拿到整行宽——
                        在 214px 的栏里，名字、标签、计数、删除挤在一行，被截的总是名字。 */}
                    <span className="wsmeta">
                      {(ws.isolated || twice.has(ws.name)) && (
                        <em className="wstag">{ws.isolated ? t("隔离") : parentOf(ws.root)}</em>
                      )}
                      {t("{n} 会话", { n: ws.sessions.length })}
                    </span>
                    <span className="wsacts">
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
                        <svg viewBox="0 0 16 16" aria-hidden="true">
                          <path d="M8 3.7v8.6M3.7 8h8.6" />
                        </svg>
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
                          const opening = sessionMenu !== ws.root;
                          if (opening) {
                            menuTrigger.current = ev.currentTarget;
                            const anchor = ev.currentTarget.getBoundingClientRect();
                            setSessionMenuAt({ x: anchor.right + 8, y: anchor.top - 7 });
                          }
                          setSessionMenu(opening ? ws.root : "");
                        }}
                      >
                        <StudioIcon name="more" />
                      </button>
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
                {!shut && (
                  <div className="kids">
                {ws.sessions.slice(0, limit).map((session) => {
                    const on = session.runtimeId === active;
                    const run = session.runtimeId ? runs[session.runtimeId]?.run : undefined;
                    if (confirm === session.path) {
                      return (
                        <Confirm
                          key={session.path}
                          what={t("删除「{name}」？", { name: rowLabel(session) })}
                          hint={t(!session.runtimeId ? "连同其记录一并删除" : liveIds([session.runtimeId]).length ? "正在运行，停止后才能删除" : "它的面板会先关掉")}
                          go={t("删除")}
                          danger
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
                        data-action-click="session.open"
                        data-action-contextmenu="session.menu"
                        aria-haspopup="menu"
                        data-action-keydown={["session.open", "session.delete"]}
                        data-target={session.path}
                        className="sessrow sessrow-context"
                        role="treeitem"
                        aria-selected={on}
                        data-on={on ? "" : undefined}
                        data-live={session.runtimeId ? "" : undefined}
                        data-run={run === "idle" ? undefined : run}
                        data-just-done={session.runtimeId && justDone.has(session.runtimeId) ? "" : undefined}
                        data-busy={busy === session.path ? "" : undefined}
                        onClick={(ev) => { if (ev.detail < 2) void pick(ws, session); }}
                        onContextMenu={(ev) => {
                          if ((ev.target as HTMLElement).closest("input, textarea, [role='menu']")) return;
                          ev.preventDefault();
                          ev.stopPropagation();
                          menuTrigger.current = ev.currentTarget;
                          setSessionMenuAt({ x: ev.clientX, y: ev.clientY });
                          setSessionMenu(session.path);
                        }}
                        tabIndex={0}
                        onKeyDown={(ev) => {
                          if (ev.target !== ev.currentTarget) return;
                          if (ev.key === "Enter" || ev.key === " ") {
                            ev.preventDefault();
                            void pick(ws, session);
                          } else if (asksDelete(ev)) {
                            ev.preventDefault();
                            askDelete(session.path);
                          }
                        }}
                      >
                        <i className="pip" />
                        {editing === session.path ? (
                          <input className="sessedit" aria-label={t("重命名该会话")} placeholder={t("留空以恢复自动标题")} autoFocus
                            defaultValue={session.title || session.name}
                            onClick={(ev) => ev.stopPropagation()}
                            data-action-blur="session.rename" data-action-keydown="session.rename" data-target={session.path}
                            onBlur={(ev) => {
                              const next = ev.currentTarget.value.trim();
                              setEditing("");
                              if (next !== (session.title || session.name).trim()) onRename(session.path, next, "inline");
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
                          <span className="sesstitle" title={rowLabel(session)}
                            data-action-doubleclick="session.rename-start" data-target={session.path}
                            onDoubleClick={(ev) => { ev.stopPropagation(); setEditing(session.path); }}>
                            <span>{rowLabel(session)}</span>
                          </span>
                        )}
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
                        {sessionMenu === session.path && createPortal(
                          <div ref={sessionMenuPortal} className="session-pop" role="menu" aria-label={t("会话操作")} style={{ maxHeight: "calc(100vh / var(--zoom, 1) - 24px)", overflowY: "auto" }} onClick={(ev) => ev.stopPropagation()}>
                            <div className="session-pop-head">
                              <b>{rowLabel(session)}</b>
                              <small>{session.runtimeId && liveIds([session.runtimeId]).length ? t("执行中") : t("已完成")} · {t("本地工作区")}</small>
                            </div>
                            <div className="session-pop-group">
                              <button role="menuitem" data-action="session.pin" data-target={session.path} onClick={() => { onPin(session.path); setSessionMenu(""); }}>
                                <StudioIcon name="pin" /><span>{pinned.has(session.path) ? t("取消置顶") : t("置顶会话")}</span>
                              </button>
                              <button role="menuitem" data-action="session.rename-start" data-target={session.path} onClick={() => { setSessionMenu(""); onRename(session.path, session.title || session.name); }}>
                                <StudioIcon name="edit" /><span>{t("重命名")}</span>
                              </button>
                              {session.runtimeId && liveIds([session.runtimeId]).length > 0 && (
                                <button role="menuitem" data-action="session.pause" data-target={session.path} onClick={() => { onPause(session.runtimeId!); setSessionMenu(""); }}>
                                  <StudioIcon name="pause" /><span>{t("暂停执行")}</span>
                                </button>
                              )}
                              {session.runtimeId && (
                                <button
                                  role="menuitem"
                                  data-action="session.close"
                                  data-target={session.path}
                                  disabled={liveIds([session.runtimeId]).length > 0}
                                  title={liveIds([session.runtimeId]).length > 0 ? t("正在运行，暂停后才能关闭") : undefined}
                                  onClick={() => {
                                    const id = session.runtimeId!;
                                    setSessionMenu("");
                                    void onClose([id]).catch(onError);
                                  }}
                                >
                                  <StudioIcon name="close" /><span>{t("关闭会话")}</span>
                                </button>
                              )}
                              <button
                                role="menuitem"
                                data-action="session.archive"
                                data-target={session.path}
                                data-value={session.archived ? "restore" : "archive"}
                                disabled={!!session.runtimeId && liveIds([session.runtimeId]).length > 0}
                                onClick={() => {
                                  setSessionMenu("");
                                  void onArchive(session.path, !session.archived, session.runtimeId).catch(onError);
                                }}
                              >
                                <StudioIcon name="archive" /><span>{t(session.archived ? "取消归档" : "归档会话")}</span>
                              </button>
                            </div>
                            <div className="session-pop-divider" />
                            <div className="session-pop-group">
                              <button role="menuitem" data-action="session.export" data-target={session.path} disabled={busy === "export:" + session.path} onClick={() => void saveSession(session)}>
                                <StudioIcon name="download" /><span>{t("导出会话")}</span><small>JSON</small>
                              </button>
                            </div>
                            <div className="session-pop-divider" />
                            <div className="session-pop-group">
                              <button className="danger" role="menuitem" data-action="session.delete" data-target={session.path} onClick={() => { setConfirm(session.path); setSessionMenu(""); }}>
                                <StudioIcon name="trash" /><span>{t("删除会话")}</span>
                              </button>
                            </div>
                          </div>,
                          document.body,
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
                              data-busy={busy === copy.path ? "" : undefined}
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
                              <span className="sesstitle">
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

                {ws.sessions.length > limit && (
                  <button
                    className="sessmore"
                    data-action="workspace.sessions-more"
                    data-target={ws.root}
                    onClick={() => setShownCounts((prev) => ({ ...prev, [ws.root]: (prev[ws.root] ?? SHOWN) + SHOWN }))}
                  >
                    {t("展开显示")}
                  </button>
                )}
                  </div>
                )}
              </div>
            );
          })}
          {!shutHere && tree.length > 0 && shownTree.length === 0 && (
            <div className="ws-empty">{t("没有匹配的会话")}</div>
          )}
          {!shutHere && tree.length === 0 && (treeRead ? (
            <div className="ws-empty">{t("尚无文件夹")}</div>
          ) : (
            <TreeReading />
          ))}
          {children}
        </div>
      </div>

    </>
  );
}

// Display only, timed from this row's mount; whether the tree is read is `treeRead`.
export function TreeReading() {
  const [since] = useState(() => Date.now());
  const [now, setNow] = useState(since);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const ms = now - since;
  return (
    <div className="ws-empty ws-reading" role="status" aria-busy="true">
      <StudioIcon name="clock" />
      <span>{t("正在读取文件夹…")}</span>
      {/* Outside the live region's speech: a status that re-announces itself every second is noise. */}
      {ms >= 1000 && <span className="ws-reading-t" aria-hidden="true">{seconds(ms, 0)}</span>}
    </div>
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

// Removing a folder closes its panes, and closing one stops what it is running.
// That price is said here rather than discovered afterwards — the kernel refuses
// the removal either way, and a refusal names no pane the reader can go find.
export function removeHint(panes: number, live: number): string {
  if (panes === 0) return t("不会删除任何文件");
  if (live === 0) return t("将先关闭 {n} 个面板；不会删除任何文件", { n: panes });
  return t("其中 {live} 个对话正在运行，停止后才能移除", { live });
}

// 确认不跟原来那行抢位置：把「×」换成「移除」两个字，宽度一变就把文件夹名挤扁
// 了。整行换成一条问句，取消永远在手边，误点的代价是零。
export function Confirm({
  what,
  hint,
  go,
  danger,
  onGo,
  onCancel,
}: {
  what: string;
  hint?: string;
  go: string;
  danger?: boolean;
  onGo: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      className="wsconfirm"
      role="alertdialog"
      aria-label={what}
      data-action-keydown="layer.dismiss"
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return;
        // Dismissing the question is not stopping the run behind it.
        ev.stopPropagation();
        onCancel();
      }}
    >
      <div className="wsconfirm-t">
        <span className="q">{what}</span>
        {hint && <span className="h">{hint}</span>}
      </div>
      <div className="wsconfirm-a">
        <button data-action="layer.dismiss" onClick={onCancel}>{t("取消")}</button>
        <button autoFocus data-action="workspace.remove" data-danger={danger ? "" : undefined} onClick={onGo}>
          {go}
        </button>
      </div>
    </div>
  );
}
