import { memo, useState } from "react";
import { useRailQuery } from "./railsearch";
import { t } from "../i18n";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";
import { REMOTE_STEP_LABEL, REMOTE_STEPS, type RemoteHost } from "../port/remote";
import { RemoteDirs } from "./RemoteDirs";
import { workspacesOf } from "./Remotes";
import { Cross } from "./glyphs";
import { StudioIcon } from "./StudioIcon";
import { Confirm } from "./Workspaces";
import { clearDraftForSession } from "./drafts";

// The same ceiling the local column uses. A machine worked on for months holds
// thousands of conversations, and drawing them all is what put 98k nodes in a
// sidebar — the reader is looking at the recent end either way.
const SHOWN = 30;

interface Props {
  hub: HubPort;
  hosts: RemoteHost[];
  runtimes: RuntimeView[];
  active: string;
  onOpen: (host: string, workspace?: string, sessionPath?: string) => Promise<void>;
  onFocus: (id: string) => void;
  // Re-reads the host book. Dropping a folder edits it, and the list this
  // column draws from is held one level up.
  reload: () => Promise<void>;
  // Each connected machine's own session book, and a way to ask for them again.
  // Held one level up with this machine's, because what makes either of them
  // stale is the same event — a turn ending — and one owner answers for both.
  trees: Record<string, TreeWorkspace[] | null>;
  reloadTrees: () => Promise<void>;
  // One machine's book, read whether or not a pane is open on it.
  readTree: (host: string) => Promise<void>;
  // Closes panes; a conversation open in an idle one is closed before it is erased.
  onClose: (ids: string[]) => Promise<void>;
  // Which of these panes are mid-turn; those are never closed to make way.
  liveIds: (ids: string[]) => string[];
  onError: (e: unknown) => void;
}

// A cold connect installs a binary on the far side, so the wait is measured in
// tens of seconds. A spinner cannot tell "downloading 40MB" from "stuck", which
// is the whole reason the kernel reports a step at all.
function Steps({ step, detail }: { step: string; detail?: string }) {
  const at = REMOTE_STEPS.indexOf(step as (typeof REMOTE_STEPS)[number]);
  if (at < 0) {
    // A step this build has no label for is shown as itself: a list that
    // silently skips one reads as a connect that stalled.
    return (
      <ul className="rmtsteps">
        <li data-at="now">
          <span className="mk">⟳</span>
          <span className="lb">{step}</span>
          {detail ? <span className="dt">{detail}</span> : null}
        </li>
      </ul>
    );
  }
  return (
    <ul className="rmtsteps">
      {REMOTE_STEPS.filter((s) => s !== "reuse").map((s, i) => {
        const state = i < at ? "done" : i === at ? "now" : "next";
        return (
          <li key={s} data-at={state}>
            <span className="mk" aria-hidden="true">
              {state === "done" ? "✓" : state === "now" ? "⟳" : "○"}
            </span>
            <span className="lb">{t(REMOTE_STEP_LABEL[s] ?? s)}</span>
            {state === "now" && detail ? <span className="dt">{detail}</span> : null}
          </li>
        );
      })}
    </ul>
  );
}

// The last segment of a path written for the other machine's rules, which are
// not this one's: a Windows host answers with backslashes to a mac.
const leaf = (dir: string) => dir.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || dir;

// A folder under one machine, and where its row came from. Only what this
// window's book holds can be dropped from here: a folder the far kernel
// reports is that machine's own note, and removing it here would take away a
// row that comes straight back on the next read.
export interface RemoteWorkspace extends TreeWorkspace {
  booked?: boolean;
}

// What the sidebar lists under one machine. The far kernel answers for what it
// remembers, and the book is what this window can offer before there is a link
// to ask through — so a folder added but never opened is still reachable, and
// one opened over there still appears without having been written down here.
export function remoteWorkspaces(host: RemoteHost, tree: TreeWorkspace[] | null | undefined): RemoteWorkspace[] {
  const known = workspacesOf(host);
  const out: RemoteWorkspace[] = tree ? tree.map((ws) => ({ ...ws, booked: known.includes(ws.root) })) : [];
  for (const dir of known) {
    if (!out.some((ws) => ws.root === dir)) out.push({ root: dir, name: leaf(dir), sessions: [], booked: true });
  }
  return out;
}

// The local list's rule: a hit on the folder keeps all of it, otherwise only
// the conversations whose titles match survive.
function makeHit(needle: string) {
  return (ws: RemoteWorkspace): RemoteWorkspace | null => {
    if (!needle) return ws;
    if (ws.name.toLowerCase().includes(needle)) return ws;
    const sessions = ws.sessions.filter((x) => (x.title || x.name || "").toLowerCase().includes(needle));
    return sessions.length ? { ...ws, sessions } : null;
  };
}

// What the pip alone cannot say. Degraded is the one that matters: the link is
// up, so nothing looks wrong until a button does nothing.
function note(host: RemoteHost): string {
  switch (host.status) {
    case "reconnecting":
      return host.attempt ? t("连接已断开，第 {n} 次重连", { n: host.attempt }) : t("连接已断开，正在重连");
    case "degraded":
      return host.error || t("已连接，但部分转发未建立");
    case "stopped":
      return host.error || t("已断开");
    default:
      return "";
  }
}

function RemoteHostsView({ hub, hosts, runtimes, active, onOpen, onFocus, reload, trees, reloadTrees, readTree, onClose, liveIds, onError }: Props) {
  const [busy, setBusy] = useState("");
  // The conversation whose deletion is being confirmed, keyed by host and path.
  const [confirm, setConfirm] = useState("");
  // The machine whose folder picker is open, empty for none. One at a time:
  // it is a dialog over the window, not a panel inside a row.
  const [picking, setPicking] = useState<RemoteHost | null>(null);
  // What each connected machine holds. Absent until asked; null while nothing
  // is open on it, which is the state the connect button belongs to.
  // Folded rows. A host's key is its name; a workspace's carries the host, so
  // one machine collapsing never takes a folder on another with it.
  const [shut, setShut] = useState<Set<string>>(new Set());
  const needle = useRailQuery();
  const hit = makeHit(needle);
  // Workspaces the reader asked to see in full.
  const [whole, setWhole] = useState<Set<string>>(new Set());

  const dropSession = async (host: RemoteHost, sessionPath: string, held?: RuntimeView) => {
    setConfirm("");
    setBusy(host.name + sessionPath);
    try {
      // An idle pane is closed first; a running one is left for the far kernel
      // to refuse, since a delete never stops work in progress.
      if (held && liveIds([held.id]).length === 0) await onClose([held.id]);
      await hub.removeRemoteSession(host.name, sessionPath);
      clearDraftForSession(host.name, sessionPath);
      await readTree(host.name);
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  const open = async (host: RemoteHost, workspace?: string, sessionPath?: string) => {
    setBusy(host.name + (sessionPath ?? workspace ?? ""));
    try {
      await onOpen(host.name, workspace ?? host.workspace, sessionPath);
      await reloadTrees();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  // Dropping a folder from this window's book. Nothing on the far machine is
  // touched, which is what makes it a one-click action rather than a question.
  const drop = async (host: string, dir: string) => {
    setBusy(host + dir);
    try {
      // Idle panes in the folder close here so this list stays in step; the
      // kernel refuses the removal while any of them is running.
      const open = runtimes.filter((rt) => rt.host === host && rt.root === dir).map((rt) => rt.id);
      if (open.length && liveIds(open).length === 0) await onClose(open);
      await hub.removeRemoteWorkspace(host, dir);
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  const readBook = async (host: string) => {
    setBusy("read:" + host);
    try {
      await readTree(host);
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  const fold = (key: string) =>
    setShut((held) => {
      const next = new Set(held);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  if (!hosts.length) return null;

  return (
    <div className="rmts">
      {picking ? (
        <RemoteDirs
          hub={hub}
          host={picking.name}
          start={picking.workspace}
          onClose={() => setPicking(null)}
          onPick={(dir) => {
            const host = picking;
            setPicking(null);
            // The kernel writes the folder into the book as it opens it, and
            // the caller re-reads the book once that answer is in — so nothing
            // here has to make a second call that could half-fail.
            void open(host, dir);
          }}
        />
      ) : null}
      {hosts.map((host) => {
        // Panes on this machine, which is also what makes the connection worth
        // holding: the link goes down with the last of them.
        const panes = runtimes.filter((rt) => rt.host === host.name);
        const tree = trees[host.name];
        const all = remoteWorkspaces(host, tree);
        // The search asks the whole list, so this half follows the local rule: a
        // hit on the machine keeps everything under it. A machine that has not
        // connected has only its name to match on, because its projects are on
        // the far side and this window cannot answer for them.
        const named = host.name.toLowerCase().includes(needle) || host.target.toLowerCase().includes(needle);
        const spaces = !needle || named ? all : (all.map(hit).filter(Boolean) as RemoteWorkspace[]);
        if (needle && !named && !spaces.length) return null;
        const folded = needle ? false : shut.has(host.name);
        const working = host.status === "connecting" || host.status === "reconnecting";
        const hint = note(host);
        return (
          <div className="rmt" key={host.name} data-state={host.status}>
            <div
              className="machrow"
              title={host.target}
              role="treeitem"
              aria-level={1}
              aria-expanded={!folded}
              onClick={() => fold(host.name)}
            >
              <button className="twist" tabIndex={-1} aria-hidden="true">
                <svg viewBox="0 0 10 10">
                  <path d="M3.4 1.6 6.8 5 3.4 8.4" />
                </svg>
              </button>
              <i className="rmtpip" aria-hidden="true" />
              <span className="rmtname">{host.name}</span>
              <span className="rmttarget" dir="ltr">{host.target}</span>
              {/* 数的是开着的面板，所以静止时每台机器都会说「0 会话」。零说不出
                  任何行看不出的事，于是不说。 */}
              {(note(host) || panes.length > 0) && (
                <span className="rmtsub">{note(host) || t("{n} 会话", { n: panes.length })}</span>
              )}
              <span className="machacts">
              {/* Reads what the machine holds without opening anything on it;
                  the same button re-reads once the list is there. */}
              <button
                className="machpick"
                data-action="remote.read"
                data-target={host.name}
                data-busy={busy === "read:" + host.name ? "" : undefined}
                disabled={!!busy}
                title={t(tree ? "刷新 {name} 上的会话" : "读取 {name} 上的会话", { name: host.name })}
                aria-label={t(tree ? "刷新 {name} 上的会话" : "读取 {name} 上的会话", { name: host.name })}
                onClick={(ev) => {
                  ev.stopPropagation();
                  void readBook(host.name);
                }}
              >
                <svg viewBox="0 0 16 16" aria-hidden="true">
                  <path d="M12.6 5.4A5 5 0 1 0 13 9.4M12.6 2.6v2.8H9.8" />
                </svg>
              </button>
              {/* Always drawn, never only on hover: an entry nobody can see is
                  read as a feature this build does not have. */}
              <button
                className="machpick"
                disabled={!!busy}
                title={t("在 {name} 上选择要打开的目录", { name: host.name })}
                aria-label={t("在 {name} 上选择要打开的目录", { name: host.name })}
                onClick={(ev) => {
                  ev.stopPropagation();
                  setPicking(host);
                }}
              >
                <svg viewBox="0 0 16 16" aria-hidden="true">
                  <path d="M8 3.7v8.6M3.7 8h8.6" />
                </svg>
              </button>
              </span>
            </div>
            {hint && !folded ? <div className="rmtnote">{hint}</div> : null}
            {working && host.step && !folded ? <Steps step={host.step} detail={host.detail} /> : null}

            {/* Connected: the machine answers for itself, so its own folders
                and conversations are what the reader picks from. Cold, the same
                rows come from the book — a project is a row you click either
                way, and connecting is what clicking one does. */}
            {folded ? null : spaces.length
              ? spaces.map((ws) => {
                  const key = host.name + ":" + ws.root;
                  const folded = needle ? false : shut.has(key);
                  return (
                    <div key={key} className="rmtws-node">
                      <div
                        className="wsrow rmtwsrow"
                        role="treeitem"
                        aria-level={2}
                        aria-expanded={!folded}
                        onClick={() => fold(key)}
                      >
                        <button className="twist" tabIndex={-1} aria-hidden="true">
                          <svg viewBox="0 0 10 10">
                            <path d="M3.4 1.6 6.8 5 3.4 8.4" />
                          </svg>
                        </button>
                        <i className="wsdot" aria-hidden="true" />
                        <span className="wsname" title={ws.root} dir="ltr">
                          {ws.name}
                        </span>
                        {/* Only the far kernel knows what it holds. Cold, "0
                            会话" would be this window guessing, and guessing
                            zero about a project worked in for months. */}
                        {tree && ws.sessions.length > 0 ? (
                          <span className="wsmeta">{t("{n} 会话", { n: ws.sessions.length })}</span>
                        ) : null}
                        <span className="wsacts">
                          <button
                            className="wsadd"
                            data-action="session.new"
                            data-busy={busy === host.name + ws.root ? "" : undefined}
                            disabled={!!busy}
                            title={t("在 {name} 下新建会话", { name: ws.name })}
                            aria-label={t("在 {name} 下新建会话", { name: ws.name })}
                            onClick={(ev) => {
                              ev.stopPropagation();
                              void open(host, ws.root);
                            }}
                          >
                            <svg viewBox="0 0 16 16" aria-hidden="true">
                              <path d="M8 3.7v8.6M3.7 8h8.6" />
                            </svg>
                          </button>
                          {/* Only a folder this window wrote down: one the far
                              kernel reports is that machine's own note, and it
                              would come straight back on the next read. */}
                          {ws.booked ? (
                            <button
                              className="wsdel"
                    data-action="remote-host.remove"
                              disabled={!!busy}
                              title={t("不再列出 {path}", { path: ws.root })}
                              aria-label={t("不再列出 {path}", { path: ws.root })}
                              onClick={(ev) => {
                                ev.stopPropagation();
                                void drop(host.name, ws.root);
                              }}
                            >
                              <Cross />
                            </button>
                          ) : null}
                        </span>
                      </div>
                      {!folded && (
                        <div className="kids">
                          {(whole.has(key) ? ws.sessions : ws.sessions.slice(0, SHOWN)).map((session) => {
                            // A conversation this window already drives is a
                            // pane to focus, never a second writer for one file.
                            const held = panes.find((rt) => rt.sessionPath === session.path);
                            const name = session.title || session.name;
                            if (confirm === host.name + session.path) {
                              return (
                                <Confirm
                                  key={session.path}
                                  what={t("删除「{name}」？", { name })}
                                  hint={t(!held ? "连同其记录一并删除" : liveIds([held.id]).length ? "正在运行，停止后才能删除" : "它的面板会先关掉") + " · " + host.name}
                                  go={t("删除")}
                                  danger
                                  onGo={() => void dropSession(host, session.path, held)}
                                  onCancel={() => setConfirm("")}
                                />
                              );
                            }
                            return (
                              <div
                                data-action="session.open"
                                key={session.path}
                                className="sessrow"
                                role="treeitem"
                                aria-level={3}
                                aria-selected={held?.id === active}
                                data-on={held?.id === active ? "" : undefined}
                                data-live={held ? "" : undefined}
                                data-busy={busy === host.name + session.path ? "" : undefined}
                                onClick={() => (held ? onFocus(held.id) : void open(host, ws.root, session.path))}
                              >
                                <i className="pip" />
                                <span className="sesstitle">{name}</span>
                                <span className="sessmeta">{session.turns ? t("{n} 轮", { n: session.turns }) : t("空会话")}</span>
                                <button
                                  className="session-more sessdel"
                                  data-action="session.delete"
                                  data-target={session.path}
                                  title={t("删除会话")}
                                  aria-label={t("删除会话：{title}", { title: name })}
                                  disabled={busy !== ""}
                                  onClick={(ev) => {
                                    ev.stopPropagation();
                                    setConfirm(host.name + session.path);
                                  }}
                                >
                                  <StudioIcon name="trash" />
                                </button>
                              </div>
                            );
                          })}
                          {ws.sessions.length > SHOWN && !whole.has(key) && (
                            <button
                              className="rmtmore"
                              onClick={() =>
                                setWhole((held) => {
                                  const next = new Set(held);
                                  next.add(key);
                                  return next;
                                })
                              }
                            >
                              {t("还有 {n} 个 · 全部显示", { n: ws.sessions.length - SHOWN })}
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })
              : panes.map((rt) => (
                  <div
                    data-action="pane.activate"
                    key={rt.id}
                    className="sessrow"
                    role="treeitem"
                    aria-level={3}
                    aria-selected={rt.id === active}
                    data-on={rt.id === active ? "" : undefined}
                    data-live=""
                    onClick={() => onFocus(rt.id)}
                  >
                    <i className="pip" />
                    <span className="sesstitle">{rt.name}</span>
                  </div>
                ))}

            {/* Only a machine with no folder written down at all. Once one is,
                its row is the connect button, and a second one beside it would
                be a different way to do the same thing. */}
            {!spaces.length && !folded && (
              <button
                data-action="remote.open"
                className="rmtopen"
                data-busy={busy.startsWith(host.name) ? "" : undefined}
                disabled={!!busy}
                title={t("该主机尚未设置默认工作区")}
                onClick={() => void open(host)}
              >
                <span className="plus" aria-hidden="true">
                  <svg viewBox="0 0 16 16">
                    <path d="M8 3.7v8.6M3.7 8h8.6" />
                  </svg>
                </span>
                {host.workspace ? (
                  <span className="rmtws" dir="ltr">
                    {host.workspace}
                  </span>
                ) : (
                  <span className="rmtws">{t("新会话")}</span>
                )}
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}

export const RemoteHosts = memo(RemoteHostsView);
