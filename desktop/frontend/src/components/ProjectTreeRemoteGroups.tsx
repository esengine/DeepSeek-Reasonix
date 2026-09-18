import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type Dispatch, type SetStateAction } from "react";
import { Plus, Server, Square, XCircle } from "lucide-react";

import { app, onRemoteTabOpened, onRemoteTabUpdated } from "../lib/bridge";
import { runtimeStateStore, selectRuntime, type RuntimeProjection } from "../lib/runtimeStateStore";
import type { Translator } from "../lib/i18n";
import type { ProjectNode, RemoteServerView, RemoteSessionView, RemoteTabRefView } from "../lib/types";
import type { ToastContextValue } from "../lib/toast";
import { loadRemoteSessionCache, removeRemoteSessionCache, saveRemoteSessionCache } from "../lib/remoteSessionCache";
import { useRemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import { publishNavigationIntent } from "../lib/useNavigationIntentFence";
import { useRemoteStore, waitForRemoteConnection } from "../store/remote";
import type { ContextMenuItem } from "./ContextMenu";

export function remoteProjectKey(ref: RemoteTabRefView): string {
  return `${ref.hostId}\u0000${ref.workspace}`;
}

// Canonical remote sessions may not have a legacy transcript path. Use the
// immutable session ID first so React keys, topic IDs, and action lookups do
// not collapse a canonical row into a path-only or blank row.
export function remoteSessionIdentity(row: Pick<RemoteSessionView, "sessionId" | "path" | "name">): string {
  return row.sessionId?.trim() || row.path?.trim() || row.name?.trim() || "new";
}

// Mutations still use the historical bridge argument named `name`, but that
// argument is an identity token, not the visible title. Canonical rows must
// use sessionId; legacy rows retain their stable basename.
export function remoteSessionActionIdentity(remote: Pick<RemoteSessionView, "sessionId" | "name" | "path">): string {
  // Keep the legacy name fallback for old Serve rows, but never fall back to
  // path: the synthetic current row may have a path while still being a
  // blank, non-catalogued session.
  const legacyIdentity = remote.name || remote.sessionId || "";
  const explicit = remote.sessionId?.trim() || legacyIdentity.trim();
  if (explicit) return explicit;
  const route = remote.path?.trim() || "";
  return route.startsWith("session-id:") ? route.slice("session-id:".length).trim() : "";
}

export function remoteSessionLabel(row: Pick<RemoteSessionView, "sessionId" | "name" | "title">, t: Translator): string {
  const title = row.title?.trim();
  if (title) return title;
  // Canonical rows use `name` as an identity compatibility field, which is
  // the opaque session ID. Never expose that ID as the visible topic label.
  if (row.sessionId?.trim()) return t("projectTree.newTopic");
  return row.name?.trim() || t("projectTree.newTopic");
}

export function useRemoteRuntimeTree(tree: ProjectNode[], sessions: Record<string, RemoteSessionView[]>, t: Translator) {
  const runtime = useSyncExternalStore(runtimeStateStore.subscribe, runtimeStateStore.getSnapshot);
  const failed = useSyncExternalStore(runtimeStateStore.subscribe, runtimeStateStore.getFailed);
  return useMemo(() => mergeRemoteSessionsIntoTree(tree, sessions, t, runtime, failed), [tree, sessions, t, runtime, failed]);
}

export function activeRemoteProjectAncestorKeys(
  nodes: ProjectNode[],
  activeRemote: RemoteTabRefView,
  nodeKey: (node: ProjectNode, index: number) => string,
): string[] {
  const activeKey = remoteProjectKey(activeRemote);
  return nodes.flatMap((node, index) => node.remote && remoteProjectKey(node.remote) === activeKey ? [nodeKey(node, index)] : []);
}

export function remoteServeBadgeState(view?: RemoteServerView, busy = false): string {
  if (busy) return "serve-busy";
  if (view?.state === "ready") return "serve-ready";
  if (view?.state === "error") return "serve-error";
  if (!view || view.state === "stopped") return "serve-idle";
  return "serve-busy";
}

export function mergeRemoteSessionsIntoTree(
  tree: ProjectNode[],
  sessions: Record<string, RemoteSessionView[]>,
  t: Translator,
  runtime?: RuntimeProjection,
  failed = false,
): ProjectNode[] {
  return tree.map((node) => {
    if (!node.remote) return node;
    const rows = sessions[remoteProjectKey(node.remote)] ?? [];
    const remoteChildren = rows.map((row): ProjectNode => {
      const identity = remoteSessionIdentity(row);
      const session = runtime?.sessions.find(session => session.hostId === node.remote!.hostId && session.workspaceRoot === node.remote!.workspace && (
        row.sessionId ? session.sessionId === row.sessionId : session.sessionPath === row.path
      ));
      const state = selectRuntime(session, failed);
      const status = state.unknown ? "unknown" : state.known && state.kind !== "idle" && state.kind !== "legacy" ? state.kind : undefined;
      return ({
      key: `remote-session-${node.remote!.hostId}-${node.remote!.workspace}-${identity}`,
      kind: "topic",
      label: remoteSessionLabel(row, t),
      root: node.remote!.workspace,
      topicId: `${node.remote!.hostId}\u0000${node.remote!.workspace}\u0000${identity}`,
      sessionPath: row.path,
      turns: row.turns,
      running: state.known ? state.unknown ? false : Boolean(state.running || session!.state.pendingPrompt || session!.state.backgroundJobs) : row.running,
      status: status as ProjectNode["status"],
      lastActivityAt: row.lastActivityAt,
      pinned: row.pinned,
      remoteSession: { hostId: node.remote!.hostId, workspace: node.remote!.workspace, name: row.name, path: row.path, sessionId: row.sessionId, title: row.title, current: row.current },
      children: [],
    });
    });
    return { ...node, children: [...remoteChildren, ...(node.children ?? [])] };
  });
}

export function useRemoteSessionActions(
  sessions: Record<string, RemoteSessionView[]>,
  refresh: () => void,
  reportError: (error: unknown) => void,
) {
  const index = useMemo(() => {
    const next = new Map<string, { hostId: string; workspace: string; name: string; path?: string; sessionId?: string; writable: boolean }>();
    for (const [groupKey, rows] of Object.entries(sessions)) {
      const [hostId, workspace] = groupKey.split("\u0000");
      for (const row of rows) {
        // A canonical row is identified exactly by its sessionId; only legacy
        // rows fall back to name identity, where duplicate names stay
        // ambiguous and mutations must refuse rather than hit the wrong row.
        const writable = Boolean(row.sessionId?.trim()) || rows.filter((candidate) => candidate.name === row.name).length === 1;
        next.set(`${hostId}\u0000${workspace}\u0000${remoteSessionIdentity(row)}`, {
          hostId, workspace, name: row.name, path: row.path, sessionId: row.sessionId, writable,
        });
      }
    }
    return next;
  }, [sessions]);
  const resolve = useCallback((topicId: string) => index.get(topicId), [index]);
  const mutate = useCallback(async (
    topicId: string,
    action: (remote: { hostId: string; workspace: string; name: string; path?: string; sessionId?: string }) => Promise<unknown>,
  ) => {
    const remote = index.get(topicId);
    if (!remote) return false;
    if (!remote.writable) throw new Error("This remote service cannot identify that session precisely. Upgrade the remote Reasonix service before changing it.");
    // The synthesized current blank session intentionally has an empty name.
    // Its rename/pin/delete bindings still own that row and must decide whether
    // the requested mutation is supported; never report success without
    // invoking the backend action.
    await action(remote);
    refresh();
    return true;
  }, [index, refresh]);
  const remove = useCallback(async (topicId: string, local: () => Promise<unknown>) => {
    try {
      if (await mutate(topicId, (remote) => {
        const identity = remoteSessionActionIdentity(remote);
        // A blank, not-yet-catalogued session has no identity the Serve can
        // delete; the deterministic "name required" rejection would only
        // surface as a broken trash action.
        if (!identity) return Promise.resolve();
        return app.DeleteRemoteProjectSession(remote.hostId, remote.workspace, identity);
      })) return;
      await local();
    } catch (error) {
      reportError(error);
    }
  }, [mutate, reportError]);
  return { resolve, mutate, remove };
}

export function openRemoteSessionNode(
  remote: { hostId: string; workspace: string; name: string; path?: string; sessionId?: string; title?: string } | undefined,
  open: (ref: RemoteTabRefView, opts?: { sessionName?: string; sessionPath?: string; sessionId?: string; sessionTitle?: string; focus?: boolean }) => Promise<void>,
): boolean {
  if (!remote) return false;
  void open(remote, remote.name || remote.path || remote.sessionId
    ? { sessionName: remote.name, sessionPath: remote.path, sessionId: remote.sessionId, sessionTitle: remote.title }
    : { focus: true });
  return true;
}

export async function renameRemoteProjectTitle(root: string, title: string): Promise<boolean> {
  if (!root.startsWith("remote-project:")) return false;
  const identity = root.slice("remote-project:".length);
  const separator = identity.indexOf(":");
  if (separator < 1) throw new Error("invalid remote project identity");
  await app.SetRemoteProjectTitle(identity.slice(0, separator), identity.slice(separator + 1), title);
  return true;
}

export function useRemoteProjectGroups(
  projects: Array<{ key?: string; remote?: RemoteTabRefView }>,
  showToast: ToastContextValue["showToast"],
  expanded: Set<string>,
  query: string,
) {
  const navigateRemote = useRemoteNavigationCommand();
  const statuses = useRemoteStore((state) => state.statuses);
  const servers = useRemoteStore((state) => state.servers);
  const [sessions, setSessions] = useState<Record<string, RemoteSessionView[]>>({});
  const [groupBusy, setGroupBusy] = useState<Record<string, boolean>>({});
  const [groupError, setGroupError] = useState<Record<string, string>>({});
  const sessionLoads = useRef(new Map<string, number>());
  const sessionLoadGenerations = useRef(new Map<string, number>());
  const eligibleSessionKeys = useRef(new Set<string>());
  const groupBusyRef = useRef(new Set<string>());
  const nextLoad = useRef(0);
  const opening = useRef(new Set<string>());
  const [revision, setRevision] = useState(0);
  const groupKeys = useMemo(
    () => projects.flatMap((project) => project.remote ? [remoteProjectKey(project.remote)] : []),
    [projects],
  );

  const acceptRemoteSessionRows = useCallback((key: string, rows: RemoteSessionView[]) => {
    saveRemoteSessionCache(key, rows);
    setSessions((current) => ({ ...current, [key]: rows }));
    // A passive refresh can recover after an explicit ensure failed. Once an
    // authoritative listing succeeds, the old connection error no longer
    // describes this group (including when the successful result is empty).
    setGroupError((current) => current[key] ? { ...current, [key]: "" } : current);
  }, []);

  const recordRemoteSessionLoadError = useCallback((key: string, error: unknown) => {
    // Passive refreshes must not turn a transient Serve failure into an
    // authoritative empty listing. Keep the last successful rows/cache while
    // surfacing a retry when the group has no rows to render.
    setGroupError((current) => ({
      ...current,
      [key]: error instanceof Error ? error.message : String(error),
    }));
  }, []);

  const openRemoteProject = useCallback(async (
    ref: RemoteTabRefView,
    opts?: { newSession?: boolean; sessionName?: string; sessionPath?: string; sessionId?: string; sessionTitle?: string; focus?: boolean },
  ) => {
    const key = remoteProjectKey(ref);
    if (opening.current.has(key)) return;
    opening.current.add(key);
    try {
      const outcome = await navigateRemote(ref,
        opts?.focus ? {} : opts?.sessionName || opts?.sessionPath || opts?.sessionId
          ? { sessionName: opts.sessionName, sessionPath: opts.sessionPath, sessionId: opts.sessionId, sessionTitle: opts.sessionTitle }
          : { newSession: true });
      if (outcome.status === "cancelled") return;
      if (outcome.status === "failed") throw outcome.error;
      if (!opts?.focus) setRevision((current) => current + 1);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    } finally {
      opening.current.delete(key);
    }
  }, [navigateRemote, showToast]);

  const ensureRemoteGroupSessions = useCallback(async (hostId: string, workspace: string) => {
    const key = `${hostId}\u0000${workspace}`;
    if (groupBusyRef.current.has(key)) return;
    groupBusyRef.current.add(key);
    setGroupBusy((current) => ({ ...current, [key]: true }));
    setGroupError((current) => ({ ...current, [key]: "" }));
    // Explicit ensures and passive listings share one last-start-wins order.
    // In particular, a passive request that began before this cold start must
    // not be allowed to overwrite the authoritative rows returned here.
    const load = ++nextLoad.current;
    sessionLoads.current.set(key, load);
    sessionLoadGenerations.current.set(key, load);
    try {
      const rows = await app.EnsureRemoteProjectSessions(hostId, workspace);
      if (sessionLoadGenerations.current.get(key) !== load) return;
      acceptRemoteSessionRows(key, rows);
      void app.RemoteServerStatus(hostId, workspace).then((view) => useRemoteStore.getState().setServer(view)).catch(() => {});
    } catch (error) {
      if (sessionLoadGenerations.current.get(key) !== load) return;
      recordRemoteSessionLoadError(key, error);
    } finally {
      if (sessionLoads.current.get(key) === load) sessionLoads.current.delete(key);
      groupBusyRef.current.delete(key);
      setGroupBusy((current) => ({ ...current, [key]: false }));
    }
  }, [acceptRemoteSessionRows, recordRemoteSessionLoadError]);

  const openRemoteWindow = useCallback(async (ref: RemoteTabRefView) => {
    try {
      const state = statuses[ref.hostId]?.state;
      if (state !== "connected" && state !== "degraded") {
        await app.ConnectRemoteHost(ref.hostId);
        await waitForRemoteConnection(ref.hostId);
      }
      await publishNavigationIntent("remote-workspace");
      await app.OpenRemoteWorkspace(ref.hostId, ref.workspace);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  }, [showToast, statuses]);

  useEffect(() => onRemoteTabOpened(() => setRevision((current) => current + 1)), []);

  useEffect(() => onRemoteTabUpdated((meta) => {
    if (!meta.remote) return;
    if (runtimeStateStore.getSnapshot()?.sessions.some(session => session.tabId === meta.id && session.state.schemaVersion === 1)) return;
    const key = remoteProjectKey(meta.remote);
    if (!groupKeys.includes(key) || !eligibleSessionKeys.current.has(key)) return;
    const load = ++nextLoad.current;
    sessionLoads.current.set(key, load);
    sessionLoadGenerations.current.set(key, load);
    void app.RemoteProjectSessions(meta.remote.hostId, meta.remote.workspace)
      .then((rows) => {
        if (sessionLoadGenerations.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
          acceptRemoteSessionRows(key, rows);
        }
      })
      .catch((error) => {
        if (sessionLoadGenerations.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
          recordRemoteSessionLoadError(key, error);
        }
      })
      .finally(() => {
        if (sessionLoads.current.get(key) === load) sessionLoads.current.delete(key);
      });
  }), [acceptRemoteSessionRows, groupKeys, recordRemoteSessionLoadError]);

  useEffect(() => {
    const seeded: Record<string, RemoteSessionView[]> = {};
    for (const key of groupKeys) {
      const rows = loadRemoteSessionCache(key);
      if (rows.length > 0) seeded[key] = rows;
    }
    if (Object.keys(seeded).length > 0) {
      setSessions((current) => ({ ...seeded, ...current }));
    }
  }, [groupKeys]);

  useEffect(() => {
    void app.RemoteConnectionStatuses()
      .then((rows) => useRemoteStore.getState().hydrateStatuses(rows ?? []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    for (const key of groupKeys) {
      const [hostId, workspace] = key.split("\u0000");
      void app.RemoteServerStatus(hostId, workspace)
        .then((view) => useRemoteStore.getState().setServer(view))
        .catch(() => {});
    }
  }, [groupKeys]);

  useEffect(() => {
    const searchable = query.trim() !== "";
    const eligible = new Set(groupKeys.filter((key) => {
      const state = statuses[key.split("\u0000")[0]]?.state;
      if (state !== "connected" && state !== "degraded") return false;
      const project = projects.find((item) => item.remote && remoteProjectKey(item.remote) === key);
      return searchable || Boolean(project?.key && expanded.has(project.key));
    }));
    eligibleSessionKeys.current = eligible;
    for (const key of sessionLoads.current.keys()) {
      if (!eligible.has(key)) sessionLoads.current.delete(key);
    }
    const retained = new Set(groupKeys);
    for (const key of sessionLoadGenerations.current.keys()) {
      if (!retained.has(key)) sessionLoadGenerations.current.delete(key);
    }
    setSessions((current) => {
      if (Object.keys(current).every((key) => retained.has(key))) return current;
      const next = Object.fromEntries(Object.entries(current).filter(([key]) => retained.has(key)));
      return next;
    });
    for (const key of eligible) {
      if (sessionLoads.current.has(key)) continue;
      const [hostId, workspace] = key.split("\u0000");
      const load = ++nextLoad.current;
      sessionLoads.current.set(key, load);
      sessionLoadGenerations.current.set(key, load);
      void app.RemoteProjectSessions(hostId, workspace)
        .then((rows) => {
          if (sessionLoadGenerations.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
            acceptRemoteSessionRows(key, rows);
          }
        })
        .catch((error) => {
          if (sessionLoadGenerations.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
            recordRemoteSessionLoadError(key, error);
          }
        })
        .finally(() => {
          if (sessionLoads.current.get(key) === load) sessionLoads.current.delete(key);
        });
    }
  }, [acceptRemoteSessionRows, expanded, groupKeys, projects, query, recordRemoteSessionLoadError, revision, statuses]);

  return {
    openRemoteProject,
    openRemoteWindow,
    remoteSessions: sessions,
    remoteGroupBusy: groupBusy,
    remoteGroupError: groupError,
    ensureRemoteGroupSessions,
    setRemoteSessions: setSessions,
    remoteServers: servers,
    refreshRemoteSessions: () => setRevision((current) => current + 1),
  };
}

export function RemoteProjectEmptyState({
  busy, error, ready, isExpanded, depth, t, onEnsure,
}: {
  busy: boolean;
  error: string;
  ready: boolean;
  isExpanded: boolean;
  depth: number;
  t: Translator;
  onEnsure: () => void;
}) {
  const inner = busy ? (
    <div className="project-tree__skeleton" style={{ paddingLeft: 14 + (depth + 1) * 16 }} aria-hidden="true">
      <span className="project-tree__skeleton-bar" />
      <span className="project-tree__skeleton-bar project-tree__skeleton-bar--short" />
      <span className="project-tree__skeleton-bar" />
      <span className="project-tree__skeleton-bar project-tree__skeleton-bar--short" />
    </div>
  ) : error || !ready ? (
    <button
      type="button"
      className={`project-tree__remote-status${error ? " project-tree__remote-status--error" : ""}`}
      style={{ paddingLeft: 14 + (depth + 1) * 16 }}
      onClick={onEnsure}
    >
      {error ? t("projectTree.remoteConnectFailed") : t("projectTree.remoteConnect")}
    </button>
  ) : null;
  if (!inner) return null;
  return (
    <div className={`project-tree__children${isExpanded ? " project-tree__children--expanded" : ""}`}>
      <div className="project-tree__children-inner">{inner}</div>
    </div>
  );
}

interface RemoteMenuOptions {
  ref: RemoteTabRefView;
  t: Translator;
  closeMenu: () => void;
  openRemoteProject: (ref: RemoteTabRefView, opts?: { newSession?: boolean; sessionName?: string; sessionPath?: string; sessionId?: string; sessionTitle?: string; focus?: boolean }) => Promise<void>;
  openRemoteWindow: (ref: RemoteTabRefView) => Promise<void>;
  setRemoteSessions: Dispatch<SetStateAction<Record<string, RemoteSessionView[]>>>;
  refresh: () => Promise<void>;
  showToast: ToastContextValue["showToast"];
}

export function buildRemoteProjectMenuItems(options: RemoteMenuOptions): ContextMenuItem[] {
  const { ref, t, closeMenu, openRemoteProject, openRemoteWindow, setRemoteSessions, refresh, showToast } = options;
  const report = (error: unknown) => showToast(error instanceof Error ? error.message : String(error), "error");
  return [
    {
      key: "remote-new-session", icon: <Plus size={13} />, label: t("projectTree.newTopic"),
      onSelect: () => { closeMenu(); void openRemoteProject(ref, { newSession: true }); },
    },
    {
      key: "remote-open-window", icon: <Server size={13} />, label: t("projectTree.remoteOpenWindow"),
      onSelect: () => { closeMenu(); void openRemoteWindow(ref); },
    },
    {
      key: "remote-stop-server", icon: <Square size={13} />, label: t("projectTree.remoteStopServer"),
      onSelect: () => { closeMenu(); void app.StopRemoteServer(ref.hostId, ref.workspace).catch(report); },
    },
    {
      key: "remote-unpin", icon: <XCircle size={13} />, label: t("projectTree.remoteUnpin"),
      onSelect: () => {
        closeMenu();
        void app.RemoveRemoteProject(ref.hostId, ref.workspace).then(() => {
          removeRemoteSessionCache(remoteProjectKey(ref));
          setRemoteSessions((current) => {
            const next = { ...current };
            delete next[remoteProjectKey(ref)];
            return next;
          });
          void refresh();
        }).catch(report);
      },
    },
  ];
}
