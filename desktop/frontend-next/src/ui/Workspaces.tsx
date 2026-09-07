import { Fragment, memo, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { HubPort, RuntimeView, TreeSession, TreeWorkspace } from "../port/hub";
import type { Adder } from "./addws";
import { chord } from "./keys";

const parentOf = (root: string) => root.replace(/[/\\]+$/, "").split(/[/\\]/).slice(-2, -1)[0] ?? "";

interface Props {
  hub: HubPort;
  tree: TreeWorkspace[];
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
  onRename: (path: string, title: string) => void;
  onError: (e: unknown) => void;
  // 打开项目这个动作归 App —— 首启那条横幅按的是同一个它。
  adder: Adder;
}

// How many of a folder's sessions get a row before the rest are summarised.
// The list is newest-first, so this is the recent end. A machine that has been
// worked on for months holds thousands of these, and drawing them all put 98k
// nodes in the sidebar — more than the transcript at 20000 turns.
const SHOWN = 30;

function WorkspacesView({ hub, tree, runtimes, active, folded, reload, onFold, onOpen, onFocus, onClose, liveIds, onRename, onError, adder }: Props) {
  const [busy, setBusy] = useState("");
  const [confirm, setConfirm] = useState("");
  const [q, setQ] = useState("");
  const find = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== "k" || !(ev.metaKey || ev.ctrlKey)) return;
      ev.preventDefault();
      find.current?.focus();
      find.current?.select();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);
  // Renaming is a pencil, not a double-click: a single click already opens the
  // session, so a double one would open it twice on the way to the edit.
  const [editing, setEditing] = useState("");
  // Folders the reader asked to see in full.
  const [whole, setWhole] = useState<Set<string>>(new Set());
  // Conversations whose conflict copies the reader asked to see.
  const [spread, setSpread] = useState<Set<string>>(new Set());
  // The kernel refuses past its own ceiling either way; this only decides when
  // the button greys out instead of failing on click.
  const maxPanes = hub.maxPanes();
  const full = runtimes.length >= maxPanes;
  // Two folders can share a name — a worktree copy carries the project's own.
  // Only then is the extra word worth the room it takes.
  const twice = new Set(tree.map((w) => w.name).filter((n, i, all) => all.indexOf(n) !== i));

  // A row that is already open is a pane to focus, never a second runtime for
  // one transcript — the kernel refuses that, and it is what forked a recovery
  // branch on every save when two writers shared a file.
  const pick = async (ws: TreeWorkspace, session: TreeSession) => {
    if (session.runtimeId) {
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
      if (open.length) await onClose(open);
      await hub.removeWorkspace(ws.root);
      await reload();
    } catch (e) {
      onError(e);
    }
  };

  const dropSession = async (session: TreeSession) => {
    if (confirm !== session.path) {
      setConfirm(session.path);
      return;
    }
    setConfirm("");
    try {
      // Waited on, not just fired: the pane's runtime holds the transcript's
      // lease until it is down, and the kernel will not erase a held one.
      if (session.runtimeId) await onClose([session.runtimeId]);
      await hub.removeSession(session.path);
      await reload();
    } catch (e) {
      onError(e);
    }
  };

  // Filtering is client-side because the tree is already here: the kernel has no
  // search route, and a round trip to re-derive what the window is holding would
  // be slower than the typing. A hit on the folder keeps all of its sessions.
  const hit = (ws: TreeWorkspace) => {
    const needle = q.trim().toLowerCase();
    if (!needle) return ws;
    const named = (x: TreeSession) => (x.title || x.name || "").toLowerCase().includes(needle);
    if (ws.name.toLowerCase().includes(needle)) return ws;
    const sessions = ws.sessions.filter(named);
    return sessions.length ? { ...ws, sessions } : null;
  };
  const shownTree = q.trim() ? (tree.map(hit).filter(Boolean) as TreeWorkspace[]) : tree;

  return (
    <>
      <div className="rail-hd">
        <div className="lbl">
          {t("工作区")}<span className="c">{tree.length}</span>
        </div>
        <button
          className="addws"
          data-busy={adder.busy ? "" : undefined}
          title={t("打开或新建项目…")}
          aria-label={t("打开或新建项目…")}
          onClick={() => adder.add("rail")}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path d="M8 3.7v8.6M3.7 8h8.6" />
          </svg>
        </button>
      </div>

      <div className="wsfind">
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M7.2 3.1a4.1 4.1 0 1 0 0 8.2 4.1 4.1 0 0 0 0-8.2M10.3 10.3 13 13" />
        </svg>
        <input
          ref={find}
          value={q}
          onChange={(ev) => setQ(ev.target.value)}
          onKeyDown={(ev) => ev.key === "Escape" && setQ("")}
          placeholder={t("搜索会话 / 项目")}
          aria-label={t("搜索会话 / 项目")}
        />
        <kbd hidden={!!q}>{chord("K")}</kbd>
        <button className="clr" hidden={!q} onClick={() => setQ("")} aria-label={t("清空")}>
          ×
        </button>
      </div>

      {adder.typing === "rail" && (
        <form
          className="addpath"
          onSubmit={(ev) => {
            ev.preventDefault();
            const input = ev.currentTarget.elements.namedItem("path") as HTMLInputElement | null;
            adder.setTyping("");
            adder.addPath(input?.value ?? "");
          }}
        >
          <input name="path" autoFocus placeholder={t("文件夹的完整路径")} onBlur={() => adder.setTyping("")} />
        </form>
      )}

      <div className="scroll">
        <div role="tree" aria-label={t("工作区与会话")}>
          {shownTree.map((ws) => {
            // A fold is a resting-state preference; while a query is on it would hide
            // the very rows the query just found.
            const shut = q.trim() ? false : folded.has(ws.root);
            // Only while the question is on screen: panesOf walks every runtime.
            const doomed = confirm === ws.root ? panesOf(ws.root) : [];
            const busyPanes = liveIds(doomed).length;
            return (
              <div className="wsnode" key={ws.root} data-missing={ws.missing ? "" : undefined}>
                {confirm === ws.root ? (
                  <Confirm
                    what={`从列表移除「${ws.name}」？`}
                    hint={removeHint(doomed.length, busyPanes)}
                    go={t("移除")}
                    danger={busyPanes > 0}
                    onGo={() => void dropWorkspace(ws)}
                    onCancel={() => setConfirm("")}
                  />
                ) : (
                  <div
                    className="wsrow"
                    role="treeitem"
                    aria-expanded={!shut}
                    data-open={ws.open ? "" : undefined}
                    onClick={() => onFold(ws.root, !shut)}
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
                        disabled={full || ws.missing}
                        title={full ? t("最多同时开 {n} 个面板，先关掉一个", { n: maxPanes }) : t("在 {name} 下开一个新会话", { name: ws.name })}
                        aria-label={t("在 {name} 下开一个新会话", { name: ws.name })}
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
                        className="wsdel"
                        title={t("从列表移除（不删除任何文件）")}
                        aria-label={t("从列表移除")}
                        onClick={(ev) => {
                          ev.stopPropagation();
                          setConfirm(ws.root);
                        }}
                      >
                        ×
                      </button>
                    </span>
                  </div>
                )}

                {/* 子项自己成一块，才能拿到那条层级引导线。缩进单独说不清楚层级：
                    两级差 18px，扫一眼只读得出“有点错位”。 */}
                {!shut && (
                  <div className="kids">
                {(whole.has(ws.root) ? ws.sessions : ws.sessions.slice(0, SHOWN)).map((session) => {
                    const on = session.runtimeId === active;
                    if (confirm === session.path) {
                      return (
                        <Confirm
                          key={session.path}
                          what={`删除「${session.title || session.name}」？`}
                          hint={t(session.runtimeId ? "它的面板会先关掉" : "连同它的记录一起删掉")}
                          go="删除"
                          danger
                          onGo={() => void dropSession(session)}
                          onCancel={() => setConfirm("")}
                        />
                      );
                    }
                    const copies = session.copies ?? [];
                    const open = spread.has(session.path);
                    return (
                      <Fragment key={session.path}>
                      <div
                        data-action="session.open"
                        className="sessrow"
                        role="treeitem"
                        aria-selected={on}
                        data-on={on ? "" : undefined}
                        data-live={session.runtimeId ? "" : undefined}
                        data-busy={busy === session.path ? "" : undefined}
                        onClick={() => void pick(ws, session)}
                      >
                        <i className="pip" />
                        {editing === session.path ? (
                          <input
                            className="sessedit"
                            aria-label={t("重命名这个会话")}
                            autoFocus
                            defaultValue={session.title || session.name}
                            onClick={(ev) => ev.stopPropagation()}
                            onBlur={(ev) => {
                              setEditing("");
                              const next = ev.currentTarget.value.trim();
                              if (next && next !== (session.title || session.name)) onRename(session.path, next);
                            }}
                            onKeyDown={(ev) => {
                              if (ev.key === "Enter") ev.currentTarget.blur();
                              if (ev.key === "Escape") {
                                // Abandoning a rename is not stopping the run behind it.
                                ev.stopPropagation();
                                ev.currentTarget.value = session.title || session.name;
                                ev.currentTarget.blur();
                              }
                            }}
                          />
                        ) : (
                          <span className="sesstitle" title={session.title || session.name}>{session.title || session.name}</span>
                        )}
                        <span className="sessmeta">{session.turns ? t("{n} 轮", { n: session.turns }) : t("空会话")}</span>
                        {copies.length > 0 && (
                          <button
                            className="sesscopies"
                            aria-expanded={open}
                            title={t("这次对话被外部程序改写时留下的副本")}
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
                            {`+${copies.length}`}
                          </button>
                        )}
                        <button
                          className="sessedit-btn"
                          title={t("重命名")}
                          aria-label={t("重命名这个会话")}
                          onClick={(ev) => {
                            ev.stopPropagation();
                            setEditing(session.path);
                          }}
                        >
                          ✎
                        </button>
                        <button
                          className="wsdel"
                          title={t("删除这个会话")}
                          aria-label={t("删除这个会话")}
                          onClick={(ev) => {
                            ev.stopPropagation();
                            setConfirm(session.path);
                          }}
                        >
                          ×
                        </button>
                      </div>
                      {open &&
                        copies.map((copy) =>
                          confirm === copy.path ? (
                            <Confirm
                              key={copy.path}
                              what={t("删除这份恢复副本？")}
                              hint={t("连同它的记录一起删掉")}
                              go={t("删除")}
                              danger
                              onGo={() => void dropSession(copy)}
                              onCancel={() => setConfirm("")}
                            />
                          ) : (
                            <div
                              data-action="session.open"
                              key={copy.path}
                              className="sessrow sesscopy"
                              role="treeitem"
                              data-busy={busy === copy.path ? "" : undefined}
                              onClick={() => void pick(ws, copy)}
                            >
                              <i className="pip" />
                              <span className="sesstitle">{t("恢复副本")}</span>
                              <span className="sessmeta">{copy.turns ? t("{n} 轮", { n: copy.turns }) : t("空会话")}</span>
                              <button
                                className="wsdel"
                                title={t("删除这个会话")}
                                aria-label={t("删除这个会话")}
                                onClick={(ev) => {
                                  ev.stopPropagation();
                                  setConfirm(copy.path);
                                }}
                              >
                                ×
                              </button>
                            </div>
                          ),
                        )}
                      </Fragment>
                    );
                  })}

                {!whole.has(ws.root) && ws.sessions.length > SHOWN && (
                  <button
                    className="sessmore"
                    onClick={() => setWhole((prev) => new Set(prev).add(ws.root))}
                  >
                    {t("还有 {n} 个 · 全部显示", { n: ws.sessions.length - SHOWN })}
                  </button>
                )}
                  </div>
                )}
              </div>
            );
          })}
          {tree.length > 0 && shownTree.length === 0 && <div className="ws-empty">{t("没有匹配的会话")}</div>}
          {tree.length === 0 && <div className="ws-empty">{t("还没有文件夹")}</div>}
        </div>
      </div>

    </>
  );
}

// Panes report upward on every usage round, so the window repaints often; the
// tree it holds does not change nearly that often.
export const Workspaces = memo(WorkspacesView);

// Removing a folder closes its panes, and closing one stops what it is running.
// That price is said here rather than discovered afterwards — the kernel refuses
// the removal either way, and a refusal names no pane the reader can go find.
export function removeHint(panes: number, live: number): string {
  if (panes === 0) return t("不会删除任何文件");
  if (live === 0) return t("会先关掉 {n} 个面板；不会删除任何文件", { n: panes });
  return t("会先关掉 {n} 个面板，其中 {live} 个还在跑；不会删除任何文件", { n: panes, live });
}

// 确认不跟原来那行抢位置：把「×」换成「移除」两个字，宽度一变就把文件夹名挤扁
// 了。整行换成一条问句，取消永远在手边，误点的代价是零。
function Confirm({
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
    <div className="wsconfirm" role="alertdialog" aria-label={what}>
      <div className="wsconfirm-t">
        <span className="q">{what}</span>
        {hint && <span className="h">{hint}</span>}
      </div>
      <div className="wsconfirm-a">
        <button onClick={onCancel}>{t("取消")}</button>
        <button autoFocus data-action="workspace.remove" data-danger={danger ? "" : undefined} onClick={onGo}>
          {go}
        </button>
      </div>
    </div>
  );
}
