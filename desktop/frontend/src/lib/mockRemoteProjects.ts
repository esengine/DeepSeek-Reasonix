import type { ProjectNode, RemoteProjectView, RemoteSessionView, TabMeta } from "./types";
import type { RemoteProjectBindings } from "./remoteProjectBridge";
import { __emitMockRemoteTab, __emitMockRemoteTabOpened } from "./remoteTabEvents";

export function createMockRemoteProjects(): {
  bindings: RemoteProjectBindings;
  appendToTree: (tree: ProjectNode[]) => ProjectNode[];
} {
  let projects: RemoteProjectView[] = [{ hostId: "demo", workspace: "~/app" }];
  const sessions: Record<string, RemoteSessionView[]> = {
    "demo\u0000~/app": [
      { name: "intro", title: "远程会话演示 / Remote demo session", turns: 18, current: true, lastActivityAt: Date.now() - 45 * 60 * 1000 },
    ],
  };
  const key = (hostId: string, workspace: string) => `${hostId}\u0000${workspace}`;
  const tabs = new Map<string, TabMeta>();
  const tabIdFor = (hostId: string, workspace: string) => `remote-mock-${hostId}-${workspace}`.replace(/[^a-z0-9-]/gi, "_");

  const bindings: RemoteProjectBindings = {
    async AddRemoteProject(hostId, workspace) {
      const existing = projects.find((project) => project.hostId === hostId && project.workspace === workspace);
      if (existing) return { ...existing, merged: true };
      const project = { hostId, workspace, title: `${hostId}:${workspace}` };
      projects = [...projects, project];
      return project;
    },
    async RemoveRemoteProject(hostId, workspace) {
      projects = projects.filter((project) => project.hostId !== hostId || project.workspace !== workspace);
    },
    async ListRemoteProjects() {
      return projects.slice();
    },
    async OpenRemoteProjectTab(hostId, workspace, opts) {
      const id = tabIdFor(hostId, workspace);
      let tab = tabs.get(id);
      if (!tab) {
        const workspaceName = workspace.split("/").filter(Boolean).pop() || workspace;
        tab = {
          id,
          scope: "project",
          workspaceRoot: workspace,
          workspaceName,
          topicId: "",
          topicTitle: workspaceName,
          label: "remote/mock",
          ready: true,
          running: false,
          mode: "normal",
          active: true,
          cwd: workspace,
          remote: { hostId, workspace },
          remoteState: "ready",
        };
        tabs.set(id, tab);
      }
      if (opts?.newSession) tab.topicTitle = "New session";
      if (opts?.sessionName) {
        const rows = sessions[key(hostId, workspace)] ?? [];
        tab.topicTitle = rows.find((row) => row.name === opts.sessionName)?.title || tab.workspaceName;
        for (const row of rows) row.current = row.name === opts.sessionName;
      }
      __emitMockRemoteTab(id, "state", { state: "ready" });
      __emitMockRemoteTabOpened({ ...tab });
      return { ...tab };
    },
    async RemoteProjectSessions(hostId, workspace) {
      return (sessions[key(hostId, workspace)] ?? []).map((row) => ({ ...row }));
    },
    async SetRemoteSessionPinned(hostId, workspace, name, pinned) {
      const row = (sessions[key(hostId, workspace)] ?? []).find((item) => item.name === name);
      if (row) row.pinned = pinned;
    },
    async SetRemoteProjectTitle(hostId, workspace, title) {
      const project = projects.find((item) => item.hostId === hostId && item.workspace === workspace);
      if (project) project.title = title.trim() || undefined;
    },
    async RenameRemoteProjectSession(hostId, workspace, name, title) {
      const row = (sessions[key(hostId, workspace)] ?? []).find((item) => item.name === name);
      if (row) row.title = title.trim();
    },
    async DeleteRemoteProjectSession(hostId, workspace, name) {
      sessions[key(hostId, workspace)] = (sessions[key(hostId, workspace)] ?? []).filter((item) => item.name !== name);
    },
    async CloseRemoteTab(tabId) { tabs.delete(tabId); },
    async SubmitRemoteTab(tabId, text) {
      __emitMockRemoteTab(tabId, "event", { kind: "turn_started" });
      __emitMockRemoteTab(tabId, "event", { kind: "message", text: `Mock remote reply: ${text}` });
      __emitMockRemoteTab(tabId, "event", { kind: "turn_done" });
    },
    async ClearRemoteTabSession(tabId) {
      __emitMockRemoteTab(tabId, "state", { state: "ready" });
    },
    async CancelRemoteTab(tabId) { __emitMockRemoteTab(tabId, "event", { kind: "turn_done" }); },
    async ApproveRemoteTab() {},
    async ResolveRemoteTabPlanDecision() {},
    async SetRemoteTabQualityFloor() {},
    async AnswerRemoteTab() {},
    async SubmitRemoteTabExtensionForm() {},
    async SetRemoteTabModel(tabId, ref) {
      const tab = tabs.get(tabId);
      if (tab) {
        tab.label = ref;
        __emitMockRemoteTabOpened({ ...tab });
      }
    },
    async RewindRemoteTab() {},
    async SetRemoteTabGoal() {},
    async RemoteTabSnapshot(tabId) {
      return { history: [], status: { label: tabs.get(tabId)?.label ?? "" } };
    },
    async RemoteTabStatus() { return { running: false, pendingPrompt: false, backgroundJobs: 0 }; },
    async SetRemoteTabEffort() {},
    async PauseRemoteTabGoal() {},
    async ResumeRemoteTabGoal() {},
    async CancelRemoteTabJobs() {},
    async SteerRemoteTab() {},
    async SetRemoteTabComposerProfile() { return []; },
    async SetRemoteTabToolApprovalMode() {},
    async SetRemoteTabPlanMode() {},
    async CompactRemoteTab() {},
    async ReplayRemoteTabPrompts() { return []; },
    async ForkRemoteTab() {},
    async SummarizeRemoteTab() {},
    async ForgetRemoteTab() {},
    async RemoteTabBranches() { return []; },
    async RemoteTabSkills() { return []; },
  };

  return {
    bindings,
    appendToTree(tree) {
      for (const project of projects) {
        if (tree.some((node) => node.key === `project_remote_${project.hostId}_${project.workspace}`)) continue;
        tree.push({
          key: `project_remote_${project.hostId}_${project.workspace}`,
          kind: "project",
          label: project.workspace.split("/").filter(Boolean).pop() || project.workspace,
          root: project.workspace,
          remote: { hostId: project.hostId, workspace: project.workspace },
        });
      }
      return tree;
    },
  };
}
