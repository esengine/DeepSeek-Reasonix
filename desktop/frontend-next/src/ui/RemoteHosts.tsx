import { memo, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { hidesLegacySessions, onHidesLegacySessionsChange } from "../state/prefs";
import { useRailQuery } from "./railsearch";
import { t } from "../i18n";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";
import { workspaceContains } from "../port/hub";
import { REMOTE_STEP_LABEL, type RemoteHost } from "../port/remote";
import { RemoteProjectMenu, type RemoteProjectMenuAnchor } from "./RemoteProjectMenu";
import type { SessionScope } from "./sessionScope";
import { remoteProjectKey, remoteProjects } from "./remoteProjects";
import { workspacesOf } from "./Remotes";
export { remoteProjectKey, remoteWorkspaces } from "./remoteProjects";
import { StudioIcon } from "./StudioIcon";
import { Confirm } from "./WorkspaceConfirm";
import { UnreadCount } from "./UnreadMark";
import { RowCount } from "./RowCount";
import { clearDraftForSession } from "./drafts";
import { RemoteSessionRow } from "./RemoteSessionRow";
import { captureSessionFocus, useSessionMutation } from "./useSessionMutation";

// The same ceiling the local column uses. A machine worked on for months holds
// thousands of conversations, and drawing them all is what put 98k nodes in a
// sidebar — the reader is looking at the recent end either way.

interface Props {
  hub: HubPort;
  hosts: RemoteHost[];
  scope?: SessionScope;
  previewLimit?: number;
  folded?: Set<string>;
  onFold?: (key: string, folded: boolean) => void;
  listedIds?: (ids: string[]) => string[];
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

function RemoteHostsView({ hub, hosts, runtimes, active, onOpen, onFocus, reload, trees, reloadTrees, readTree, onClose, liveIds, listedIds = liveIds, scope = "all", previewLimit = Infinity, folded: foldedProjects, onFold, onError }: Props) {
  const [busy, setBusy] = useState("");
  const [projectMenu, setProjectMenu] = useState<RemoteProjectMenuAnchor>();
  // The conversation whose deletion is being confirmed, keyed by host and path.
  const [confirm, setConfirm] = useState("");
  const deletionFocus = useRef<(() => void) | null>(null);
  const mutations = useSessionMutation(onError);
  useLayoutEffect(() => {
    if (confirm || !deletionFocus.current) return;
    deletionFocus.current();
    deletionFocus.current = null;
  }, [confirm]);
  // A remote project is identified by both host and root, including its fold.
  const [shut, setShut] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const needle = useRailQuery();
  const hideLegacy = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);

  const dropSession = async (host: RemoteHost, sessionPath: string, held?: RuntimeView) => {
    const restore = deletionFocus.current ?? undefined;
    deletionFocus.current = null;
    setConfirm("");
    await mutations.run(JSON.stringify([host.name, sessionPath]), "delete", null, async () => {
      // An idle pane is closed first; a running one is left for the far kernel
      // to refuse, since a delete never stops work in progress.
      if (held && liveIds([held.id]).length === 0) await onClose([held.id]);
      await hub.removeRemoteSession(host.name, sessionPath);
      clearDraftForSession(host.name, sessionPath);
      await readTree(host.name);
    }, restore);
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
  const drop = async (host: RemoteHost, workspace: TreeWorkspace) => {
    setBusy(host.name + workspace.root);
    try {
      const open = runtimes.filter((rt) => rt.host === host.name && workspaceContains(workspace, rt.root)).map((rt) => rt.id);
      const roots = [...new Set([workspace.root, ...(workspace.workspaceRoots ?? []),
        ...workspacesOf(host).filter((root) => workspaceContains(workspace, root))])];
      await hub.removeRemoteWorkspace(host.name, workspace.root, roots);
      if (open.length) await onClose(open);
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setBusy("");
    }
  };

  // The rail lists machines in the book's order, so a step is a placement in the
  // book relative to the machines shown here, which may skip some that are hidden.
  const place = async (host: RemoteHost, before: string) => {
    setBusy("move:" + host.name);
    try {
      await hub.placeRemoteHost(host.name, before);
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
      await reload();
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

  return (
    <>
      {hosts.flatMap((host, position) => {
        const moves = {
          ...(position > 0 ? { [-1]: () => void place(host, hosts[position - 1].name) } : {}),
          ...(position < hosts.length - 1 ? { 1: () => void place(host, hosts[position + 2]?.name ?? "") } : {}),
        };
        const panes = runtimes.filter((rt) => rt.host === host.name);
        const tree = trees[host.name];
        const spaces = remoteProjects(host, tree, panes, scope, needle, listedIds, hideLegacy);
        const working = host.status === "connecting" || host.status === "reconnecting";
        const hint = note(host);
        return spaces.map(({ workspace: ws, loose, older }) => {
          const key = remoteProjectKey(host.name, ws.root);
          const hasChildren = ws.sessions.length > 0 || (ws.total ?? 0) > 0 || (!tree && !working) || panes.some((rt) => workspaceContains(ws, rt.root));
          const folded = hasChildren && !needle && (foldedProjects ?? shut).has(key);
          const currentPane = panes.find((rt) => workspaceContains(ws, rt.root) && rt.id === active);
          const current = !!currentPane;
          const visible = needle || scope !== "all" || expanded.has(key) ? ws.sessions
            : ws.sessions.filter((session, index) => index < previewLimit || panes.some((rt) => rt.id === active && rt.sessionPath === session.path));
          return (
            <div key={key} className="wsnode rmtws-node" data-state={host.status}
              data-current={current ? "" : undefined}>
              <div className="wsrow rmtwsrow" role="treeitem" aria-level={1} aria-expanded={hasChildren ? !folded : undefined}
                aria-haspopup="menu" tabIndex={-1} data-action-contextmenu="workspace.menu" data-action-keydown="workspace.menu"
                onContextMenu={(event) => {
                  if ((event.target as HTMLElement).closest("input, textarea, [role='menu']")) return;
                  event.preventDefault();
                  event.stopPropagation();
                  setProjectMenu({ key, trigger: event.currentTarget,
                    point: event.clientX || event.clientY ? { x: event.clientX, y: event.clientY } : undefined });
                }}
                onKeyDown={(event) => {
                  if (event.target !== event.currentTarget || (event.key !== "ContextMenu" && !(event.shiftKey && event.key === "F10"))) return;
                  event.preventDefault();
                  event.stopPropagation();
                  setProjectMenu({ key, trigger: event.currentTarget });
                }}
                data-action-click="tree.navigate" data-target={key} onClick={() => { if (hasChildren) { if (onFold) onFold(key, !folded); else fold(key); } }}>
                <StudioIcon name="folder" className="wsfolder" />
                <span className="wsname" title={host.name + " · " + ws.root} dir="ltr">{ws.name}</span>
                <span className="project-source" title={host.target}>{host.name}</span>
                {currentPane && <span className="bound-workspace" title={currentPane.root} dir="ltr">{t("连接目录：{path}", { path: currentPane.root })}</span>}
                <span className="wsacts">
                  {folded && <UnreadCount n={ws.sessions.filter((session) => session.unread).length} />}
                  {tree && ws.sessions.length > 0 && <RowCount c={scope === "all" && older > 0
                    ? { shown: ws.sessions.length, of: ws.sessions.length + older } : { shown: ws.sessions.length }} />}
                  <span className="rowbtns">
                    <button className="wsadd" data-action="session.new" data-target={key}
                      data-busy={busy === host.name + ws.root ? "" : undefined} disabled={!!busy}
                      title={t("在 {name} 下新建会话", { name: ws.name + " · " + host.name })}
                      aria-label={t("在 {name} 下新建会话", { name: ws.name + " · " + host.name })}
                      onClick={(event) => { event.stopPropagation(); void open(host, ws.root); }}>
                      <StudioIcon name="chat" />
                    </button>
                    <RemoteProjectMenu name={ws.name} root={ws.root} host={host.name} defaultRoot={host.workspace} busy={!!busy}
                      anchor={projectMenu?.key === key ? projectMenu : undefined}
                      onOpen={(trigger) => setProjectMenu({ key, trigger })} onClose={() => setProjectMenu(undefined)}
                      onRefresh={() => void readBook(host.name)}
                      onRemove={() => void drop(host, ws)} onMove={moves} />
                  </span>
                </span>
              </div>
              {host.status !== "connected" && <div className="project-connection" role="status">
                <StudioIcon name={working ? "refresh" : "warning"} />
                <span>{working ? [t("正在建立连接…"), host.step ? t(REMOTE_STEP_LABEL[host.step] ?? host.step) : ""].filter(Boolean).join(" · ") : hint || t("未连接")}</span>
              </div>}
              {!folded && hasChildren && <div className="kids">
                {visible.map((session) => {
                  const held = panes.find((rt) => rt.sessionPath === session.path);
                  const name = session.title || session.name;
                  if (confirm === host.name + session.path) return (
                    <Confirm key={session.path} what={t("删除「{name}」？", { name })}
                      hint={t(!held ? "连同其记录一并删除" : liveIds([held.id]).length ? "正在运行，停止后才能删除" : "它的面板会先关掉") + " · " + host.name}
                      go={t("删除")} danger onGo={() => void dropSession(host, session.path, held)}
                      onCancel={() => setConfirm("")} />
                  );
                  return (
                    <RemoteSessionRow key={session.path} host={host.name} target={key + ":" + session.path}
                      session={session} held={held} active={active} busy={!!busy} live={!!held && liveIds([held.id]).length > 0}
                      pending={mutations.pending.get(JSON.stringify([host.name, session.path]))}
                      onOpen={() => void open(host, session.workspaceRoot || ws.root, session.path)} onFocus={onFocus}
                      onWrite={(kind, row) => void mutations.run(JSON.stringify([host.name, session.path]), kind, row, async () => {
                        if (kind === "pin") await hub.pinSession(session.path, !session.pinned, host.name);
                        else if (kind === "read") await hub.markSessionRead(session.path, host.name);
                        else {
                          if (held && !session.archived) await onClose([held.id]);
                          await hub.archiveSession(session.path, !session.archived, host.name);
                          if (!session.archived) clearDraftForSession(host.name, session.path);
                        }
                        await readTree(host.name);
                      })}
                      onDelete={(row) => { deletionFocus.current = captureSessionFocus(row); setConfirm(host.name + session.path); }} />
                  );
                })}
                {!tree && !working && <button className="sessmore" data-action="remote.read" data-target={host.name} disabled={!!busy}
                  onClick={() => void readBook(host.name)}>{t("读取 {name} 上的会话", { name: host.name })}</button>}
                {visible.length < ws.sessions.length && <button className="sessmore" data-action="session.more"
                  onClick={() => setExpanded((value) => new Set(value).add(key))}>{t("显示更多（还有 {n} 个）", { n: ws.sessions.length - visible.length })}</button>}
                {older > 0 && scope === "all" && (
                  <small className="rmtolder">{t("还有 {n} 个较早的会话，在那台机器上查看", { n: older })}</small>
                )}
                {loose.map((rt) => <div key={rt.id} className="sessrow" role="treeitem" aria-level={2}
                    data-action="pane.activate" aria-selected={rt.id === active} data-on={rt.id === active ? "" : undefined}
                    data-live="" onClick={() => onFocus(rt.id)}>
                    <i className="pip" /><span className="sesstitle" title={rt.name}><span>{rt.name}</span></span>
                  </div>)}
              </div>}
            </div>
          );
        });
      })}
    </>
  );
}

export const RemoteHosts = memo(RemoteHostsView);
