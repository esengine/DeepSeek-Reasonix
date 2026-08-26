import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { Plus, Server, Square, XCircle } from "lucide-react";

import { app, onRemoteTabOpened, onRemoteTabUpdated } from "../lib/bridge";
import type { Translator } from "../lib/i18n";
import type { ProjectNode, RemoteServerView, RemoteSessionView, RemoteTabRefView } from "../lib/types";
import type { ToastContextValue } from "../lib/toast";
import { useRemoteStore, waitForRemoteConnection } from "../store/remote";
import type { ContextMenuItem } from "./ContextMenu";

export function remoteProjectKey(ref: RemoteTabRefView): string {
  return `${ref.hostId}\u0000${ref.workspace}`;
}

export function remoteServeBadgeState(view?: RemoteServerView): string {
  if (view?.state === "ready") return "serve-ready";
  if (view?.state === "error") return "serve-error";
  if (!view || view.state === "stopped") return "serve-idle";
  return "serve-busy";
}

export function mergeRemoteSessionsIntoTree(
  tree: ProjectNode[],
  sessions: Record<string, RemoteSessionView[]>,
  t: Translator,
): ProjectNode[] {
  return tree.map((node) => {
    if (!node.remote) return node;
    const rows = sessions[remoteProjectKey(node.remote)] ?? [];
    const remoteChildren = rows.map((row): ProjectNode => ({
      key: `remote-session-${node.remote!.hostId}-${node.remote!.workspace}-${row.name}`,
      kind: "topic",
      label: row.title || row.name || t("projectTree.newTopic"),
      topicId: `${node.remote!.hostId}\u0000${node.remote!.workspace}\u0000${row.name}`,
      turns: row.turns,
      lastActivityAt: row.lastActivityAt,
      pinned: row.pinned,
      remoteSession: { hostId: node.remote!.hostId, workspace: node.remote!.workspace, name: row.name },
      children: [],
    }));
    return { ...node, children: [...remoteChildren, ...(node.children ?? [])] };
  });
}

export function useRemoteSessionActions(
  sessions: Record<string, RemoteSessionView[]>,
  refresh: () => void,
  reportError: (error: unknown) => void,
) {
  const index = useMemo(() => {
    const next = new Map<string, { hostId: string; workspace: string; name: string }>();
    for (const [groupKey, rows] of Object.entries(sessions)) {
      const [hostId, workspace] = groupKey.split("\u0000");
      for (const row of rows) next.set(`${hostId}\u0000${workspace}\u0000${row.name}`, { hostId, workspace, name: row.name });
    }
    return next;
  }, [sessions]);
  const resolve = useCallback((topicId: string) => index.get(topicId), [index]);
  const mutate = useCallback(async (
    topicId: string,
    action: (remote: { hostId: string; workspace: string; name: string }) => Promise<unknown>,
  ) => {
    const remote = index.get(topicId);
    if (!remote) return false;
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
      if (await mutate(topicId, (remote) => app.DeleteRemoteProjectSession(remote.hostId, remote.workspace, remote.name))) return;
      await local();
    } catch (error) {
      reportError(error);
    }
  }, [mutate, reportError]);
  return { resolve, mutate, remove };
}

export function openRemoteSessionNode(
  remote: { hostId: string; workspace: string; name: string } | undefined,
  open: (ref: RemoteTabRefView, opts?: { sessionName?: string; focus?: boolean }) => Promise<void>,
): boolean {
  if (!remote) return false;
  void open(remote, remote.name ? { sessionName: remote.name } : { focus: true });
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
  const statuses = useRemoteStore((state) => state.statuses);
  const servers = useRemoteStore((state) => state.servers);
  const [sessions, setSessions] = useState<Record<string, RemoteSessionView[]>>({});
  const sessionLoads = useRef(new Map<string, number>());
  const eligibleSessionKeys = useRef(new Set<string>());
  const nextLoad = useRef(0);
  const opening = useRef(new Set<string>());
  const [revision, setRevision] = useState(0);
  const groupKeys = useMemo(
    () => projects.flatMap((project) => project.remote ? [remoteProjectKey(project.remote)] : []),
    [projects],
  );

  const openRemoteProject = useCallback(async (
    ref: RemoteTabRefView,
    opts?: { newSession?: boolean; sessionName?: string; focus?: boolean },
  ) => {
    const key = remoteProjectKey(ref);
    if (opening.current.has(key)) return;
    opening.current.add(key);
    try {
      await app.OpenRemoteProjectTab(ref.hostId, ref.workspace,
        opts?.focus ? {} : opts?.sessionName ? { sessionName: opts.sessionName } : { newSession: true });
      if (!opts?.focus) setRevision((current) => current + 1);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    } finally {
      opening.current.delete(key);
    }
  }, [showToast]);

  const openRemoteWindow = useCallback(async (ref: RemoteTabRefView) => {
    try {
      const state = statuses[ref.hostId]?.state;
      if (state !== "connected" && state !== "degraded") {
        await app.ConnectRemoteHost(ref.hostId);
        await waitForRemoteConnection(ref.hostId);
      }
      await app.OpenRemoteWorkspace(ref.hostId, ref.workspace);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  }, [showToast, statuses]);

  useEffect(() => onRemoteTabOpened(() => setRevision((current) => current + 1)), []);

  useEffect(() => onRemoteTabUpdated((meta) => {
    if (!meta.remote) return;
    const key = remoteProjectKey(meta.remote);
    if (!groupKeys.includes(key) || !eligibleSessionKeys.current.has(key)) return;
    const load = ++nextLoad.current;
    sessionLoads.current.set(key, load);
    void app.RemoteProjectSessions(meta.remote.hostId, meta.remote.workspace)
      .then((rows) => {
        if (sessionLoads.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
          setSessions((current) => ({ ...current, [key]: rows }));
        }
      })
      .catch(() => {})
      .finally(() => {
        if (sessionLoads.current.get(key) === load) sessionLoads.current.delete(key);
      });
  }), [groupKeys]);

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
    const connected = new Set(groupKeys.filter((key) => {
      const state = statuses[key.split("\u0000")[0]]?.state;
      return state === "connected" || state === "degraded";
    }));
    setSessions((current) => {
      if (Object.keys(current).every((key) => connected.has(key))) return current;
      const next = Object.fromEntries(Object.entries(current).filter(([key]) => connected.has(key)));
      return next;
    });
    for (const key of eligible) {
      if (sessionLoads.current.has(key)) continue;
      const [hostId, workspace] = key.split("\u0000");
      const load = ++nextLoad.current;
      sessionLoads.current.set(key, load);
      void app.RemoteProjectSessions(hostId, workspace)
        .then((rows) => {
          if (sessionLoads.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
            setSessions((current) => ({ ...current, [key]: rows }));
          }
        })
        .catch(() => {
          if (sessionLoads.current.get(key) === load && eligibleSessionKeys.current.has(key)) {
            setSessions((current) => ({ ...current, [key]: [] }));
          }
        })
        .finally(() => {
          if (sessionLoads.current.get(key) === load) sessionLoads.current.delete(key);
        });
    }
  }, [expanded, groupKeys, projects, query, revision, statuses]);

  return {
    openRemoteProject,
    openRemoteWindow,
    remoteSessions: sessions,
    setRemoteSessions: setSessions,
    remoteServers: servers,
    refreshRemoteSessions: () => setRevision((current) => current + 1),
  };
}

interface RemoteMenuOptions {
  ref: RemoteTabRefView;
  t: Translator;
  closeMenu: () => void;
  openRemoteProject: (ref: RemoteTabRefView, opts?: { newSession?: boolean; sessionName?: string; focus?: boolean }) => Promise<void>;
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
