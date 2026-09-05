import { CommandCancelled } from "../lib/commandOutcome";
import type { RemoteTabOpenOptions, RemoteTabRefView, SessionMeta, TabMeta } from "../lib/types";
import type { useAppRuntimeAdapter } from "./useAppRuntimeAdapter";
import { isChannelSession, sidebarImSessionTarget, type SidebarImConnection } from "./sidebarImProjection";
import type { SessionOperationAuthority } from "./useResourceOperations";

export type DesktopNavigationIntent =
  | { kind: "topic"; scope: string; workspaceRoot: string; topicId: string; sessionPath?: string }
  | { kind: "blank"; scope: string; workspaceRoot: string }
  | { kind: "isolated-worktree"; workspaceRoot: string }
  | { kind: "sidebar-im"; connection: SidebarImConnection }
  | { kind: "resume-session"; session: SessionMeta }
  | { kind: "remote-project"; remote: RemoteTabRefView; options: RemoteTabOpenOptions };
type Runtime = ReturnType<typeof useAppRuntimeAdapter>;
export type DesktopNavigationPorts = Pick<Runtime["navigation"],
  "isNavigationIntentCurrent" | "activateTopic" | "openTopicSession" | "openGlobalTab" | "openProjectTab"
  | "ensureBlankSurface" | "ensureBlankTab" | "createIsolatedWorktree" | "registeredNavigationIntent" | "switchRemoteTab"> &
  Pick<Runtime["sessionActions"], "openChannelSession" | "resumeSession"> & {
    listTabs(): Promise<TabMeta[]>;
    openRemoteProject(hostId: string, workspace: string, options: RemoteTabOpenOptions): Promise<TabMeta>;
    applyTabs(tabs: TabMeta[]): void;
    seedTab(tab: TabMeta): void;
    reveal(): void;
    projectChanged(): void;
    closeHistory(): void;
    listSessions(): Promise<SessionMeta[]>;
    applyHistorySessions(sessions: SessionMeta[]): void;
    notice(notice: NavigationNotice): void;
  };
export type NavigationNotice = {
  key: "history.failedOpenSession" | "history.missingWorkspaceRoot" | "history.failedOpenProject" | "sidebar.imWaiting" | "sidebar.imOpenFailed"
    | "projectTree.worktreeCreated" | "projectTree.worktreeCreatedDirty";
  params?: Record<string, string>;
  tone?: "error" | "warn" | "info";
  durationMs?: number;
} | { message: string; tone?: "error"; durationMs?: number };
export type DesktopNavigationCapture = {
  intent: DesktopNavigationIntent;
  navigationIntentSeq: number;
  singleSurface: boolean;
  ports: DesktopNavigationPorts;
};
class InvalidSessionTarget extends Error {
  constructor(readonly key: "history.failedOpenSession" | "history.missingWorkspaceRoot") { super(key); }
}

/** One executor for topic, blank, IM, worktree and history activation. */
export async function executeDesktopNavigation(input: DesktopNavigationCapture, authority: SessionOperationAuthority) {
  const { intent: request, navigationIntentSeq: seq, ports, singleSurface } = input;
  const checkpoint = () => {
    authority.checkpoint();
    if (!ports.isNavigationIntentCurrent(seq)) throw new CommandCancelled("superseded");
  };
  const refresh = async () => {
    const tabs = await ports.listTabs().catch(() => []);
    checkpoint();
    ports.applyTabs(tabs);
  };
  const openTopic = (scope: string, workspace: string, topic: string, path?: string) => singleSurface
    ? ports.activateTopic(scope, workspace, topic, path || "", seq)
    : path ? ports.openTopicSession(scope, workspace, topic, path, seq)
      : scope === "global" ? ports.openGlobalTab(topic, seq) : ports.openProjectTab(workspace, topic, seq);
  const openBlank = (scope: string, workspace: string) => singleSurface
    ? ports.ensureBlankSurface(scope, scope === "project" ? workspace : "", seq)
    : ports.ensureBlankTab(scope, scope === "project" ? workspace : "", seq);
  checkpoint();
  try {
    if (request.kind === "remote-project") {
      const token = await ports.registeredNavigationIntent(seq);
      checkpoint();
      if (!token) throw new CommandCancelled("superseded");
      const tab = await ports.openRemoteProject(request.remote.hostId, request.remote.workspace, request.options);
      checkpoint(); ports.seedTab(tab);
      await ports.switchRemoteTab(tab, seq);
      checkpoint(); ports.reveal();
      await refresh();
      return tab;
    }
    if (request.kind === "topic" || request.kind === "blank") {
      const tab = request.kind === "topic"
        ? await openTopic(request.scope, request.workspaceRoot, request.topicId, request.sessionPath)
        : await openBlank(request.scope, request.workspaceRoot);
      checkpoint(); ports.seedTab(tab);
      if (request.kind === "blank") ports.projectChanged();
      if (request.kind === "topic") { ports.reveal(); await refresh(); }
      else { await refresh(); checkpoint(); ports.reveal(); }
      return;
    }
    if (request.kind === "isolated-worktree") {
      const result = await ports.createIsolatedWorktree(request.workspaceRoot, seq);
      checkpoint(); ports.seedTab(result.tab); ports.projectChanged();
      await refresh(); checkpoint();
      ports.notice({ key: result.sourceDirty ? "projectTree.worktreeCreatedDirty" : "projectTree.worktreeCreated",
        params: { branch: result.branch }, tone: result.sourceDirty ? "warn" : "info", durationMs: result.sourceDirty ? 7000 : 3500 });
      ports.reveal(); return;
    }
    if (request.kind === "sidebar-im") {
      const { connection } = request;
      const target = sidebarImSessionTarget(connection);
      if (!target) { ports.notice({ key: "sidebar.imWaiting", params: { name: connection.title } }); return; }
      let tab: TabMeta;
      if (target.kind === "path") {
        tab = await openBlank(connection.scope, connection.workspaceRoot);
        checkpoint();
        if (connection.sessionSource === "auto") await ports.openChannelSession(target.value, tab.id, seq);
        else await ports.resumeSession(target.value, tab.id, seq);
      } else tab = await openTopic(connection.scope, connection.workspaceRoot, target.value);
      checkpoint(); ports.seedTab(tab);
      await refresh(); checkpoint(); ports.reveal(); ports.projectChanged(); return;
    }
    const { session } = request;
    const scope = session.scope || (session.workspaceRoot ? "project" : "global");
    let tab: TabMeta;
    if (isChannelSession(session)) {
      tab = await openBlank(scope === "project" ? "project" : "global", session.workspaceRoot || "");
      checkpoint(); await ports.openChannelSession(session.path, tab.id, seq);
    } else if (scope === "project" && session.workspaceRoot && session.topicId) {
      tab = await openTopic("project", session.workspaceRoot, session.topicId, session.path);
    } else if (scope === "global" && session.topicId) {
      tab = await openTopic("global", "", session.topicId, session.path);
    } else throw new InvalidSessionTarget(scope === "global" && !session.topicId
      ? "history.failedOpenSession" : session.topicId ? "history.missingWorkspaceRoot" : "history.failedOpenSession");
    checkpoint(); ports.seedTab(tab); ports.closeHistory();
    ports.reveal(); await refresh();
  } catch (error) {
    checkpoint();
    if (request.kind === "remote-project") throw error;
    if (request.kind === "topic" || request.kind === "blank") {
      ports.notice({ key: "history.failedOpenSession", tone: "error" });
      await refresh(); return;
    }
    if (request.kind === "isolated-worktree") {
      ports.notice({ message: error instanceof Error ? error.message : String(error), tone: "error", durationMs: 6000 }); return;
    }
    if (request.kind === "sidebar-im") { ports.notice({ key: "sidebar.imOpenFailed", params: { name: request.connection.title } }); return; }
    const history = await ports.listSessions().catch(() => null);
    checkpoint();
    if (history) ports.applyHistorySessions(history);
    const message = error instanceof Error ? error.message : String(error ?? "");
    if (/no such file|cannot find the file|file does not exist|session is pending cleanup|session .*not found/i.test(message)) return;
    ports.closeHistory();
    const session = request.session;
    const scope = session.scope || (session.workspaceRoot ? "project" : "global");
    if (scope === "project" && session.workspaceRoot) {
      const parts = session.workspaceRoot.split(/[/\\]/).filter(Boolean);
      ports.notice({ key: "history.failedOpenProject", params: {
        name: parts[parts.length - 1] || session.workspaceRoot, path: session.workspaceRoot,
      } });
    } else ports.notice(error instanceof InvalidSessionTarget ? { key: error.key } : { message });
  }
}
