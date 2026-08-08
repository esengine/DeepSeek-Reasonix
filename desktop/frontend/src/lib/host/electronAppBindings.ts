/**
 * AppBindings for Electron + multi-tab reasonix serve (--multi-tab).
 * ListTabs / *ForTab map to /tabs APIs. Terminal/remote remain stubs.
 */
import type { AppBindings } from "../bridge";
import type {
  BotSettingsView,
  ContextInfo,
  DesktopStartupSettingsView,
  EffortInfo,
  HistoryMessage,
  HistoryPage,
  Meta,
  ProjectNode,
  SettingsView,
  TabMeta,
  WorkspaceView,
} from "../types";
import { HttpSseHost } from "./httpSseHost";
import { POC_CAPABILITIES } from "./capabilities";
import { DEFAULT_STATUS_BAR_ITEMS } from "../statusBarItems";

export type ElectronServeEndpoint = {
  baseUrl: string;
  token: string;
  uiUrl?: string;
  port?: number;
  logFile?: string;
  workspace?: string;
};

function historyToPage(raw: unknown): HistoryPage {
  const messages = Array.isArray(raw) ? (raw as HistoryMessage[]) : [];
  const userTurns = messages.filter((m) => m.role === "user").length;
  return {
    messages,
    startTurn: 0,
    endTurn: Math.max(0, userTurns),
    totalTurns: Math.max(0, userTurns),
    hasOlder: false,
  };
}

function asTabMeta(raw: unknown): TabMeta | null {
  if (!raw || typeof raw !== "object") return null;
  const t = raw as TabMeta;
  if (typeof t.id !== "string" || !t.id) return null;
  return {
    ...t,
    tabType: t.tabType ?? "session",
    scope: t.scope || "project",
    workspaceRoot: t.workspaceRoot || t.cwd || "",
    workspaceName: t.workspaceName || "workspace",
    topicId: t.topicId || "main",
    topicTitle: t.topicTitle || "Session",
    label: t.label || "tab",
    ready: t.ready !== false,
    running: !!t.running,
    mode: t.mode || "agent",
    collaborationMode: t.collaborationMode || "default",
    toolApprovalMode: t.toolApprovalMode || "ask",
    tokenMode: t.tokenMode || "auto",
    active: !!t.active,
    cwd: t.cwd || t.workspaceRoot || "",
  } as TabMeta;
}

function metaFromTab(t: TabMeta): Meta {
  return {
    label: t.label,
    ready: t.ready,
    eventChannel: "agent:event",
    cwd: t.cwd || t.workspaceRoot,
    workspaceRoot: t.workspaceRoot,
    workspaceName: t.workspaceName,
    workspacePath: t.workspacePath || t.workspaceRoot,
    sessionPath: t.sessionPath,
    collaborationMode: t.collaborationMode,
    toolApprovalMode: t.toolApprovalMode,
    tokenMode: t.tokenMode,
    goal: t.goal || "",
    goalStatus: t.goalStatus || "stopped",
  };
}

/**
 * Build a sidebar project tree from open multi-tab Controllers.
 * Serve has no Wails ListProjectTree; without this the UI always shows
 * "还没有项目" even after a successful PickWorkspace / open-project.
 */
function projectTreeFromTabs(tabs: TabMeta[]): ProjectNode[] {
  type Acc = {
    root: string;
    label: string;
    topics: Map<string, { topicId: string; title: string; sessionPath?: string; running: boolean; open: boolean }>;
  };
  const projects = new Map<string, Acc>();
  for (const t of tabs) {
    if (t.scope === "global") continue;
    const root = (t.workspaceRoot || t.cwd || "").trim();
    if (!root) continue;
    let acc = projects.get(root);
    if (!acc) {
      acc = {
        root,
        label: t.workspaceName || root.split(/[/\\]/).filter(Boolean).pop() || root,
        topics: new Map(),
      };
      projects.set(root, acc);
    }
    const topicId = (t.topicId || "main").trim() || "main";
    const prev = acc.topics.get(topicId);
    acc.topics.set(topicId, {
      topicId,
      title: (t.topicTitle || prev?.title || "Session").trim() || "Session",
      sessionPath: t.sessionPath || prev?.sessionPath,
      running: !!(t.running || prev?.running),
      open: true,
    });
  }
  const out: ProjectNode[] = [];
  for (const acc of projects.values()) {
    const children: ProjectNode[] = [];
    for (const topic of acc.topics.values()) {
      children.push({
        key: `topic_${topic.topicId}`,
        kind: "topic",
        label: topic.title,
        root: acc.root,
        topicId: topic.topicId,
        sessionPath: topic.sessionPath,
        open: topic.open,
        running: topic.running,
      });
    }
    // Ensure at least one topic so the folder is expandable / clickable.
    if (children.length === 0) {
      children.push({
        key: "topic_main",
        kind: "topic",
        label: "Session",
        root: acc.root,
        topicId: "main",
        open: true,
        running: false,
      });
    }
    out.push({
      key: `project_${acc.root}`,
      kind: "project",
      label: acc.label,
      root: acc.root,
      open: true,
      children,
    });
  }
  // Stable order by label then root.
  out.sort((a, b) => {
    const la = (a.label || a.root || "").toLowerCase();
    const lb = (b.label || b.root || "").toLowerCase();
    if (la !== lb) return la < lb ? -1 : 1;
    return (a.root || "").localeCompare(b.root || "");
  });
  return out;
}

function contextFromRaw(raw: unknown): ContextInfo {
  if (!raw || typeof raw !== "object") {
    return { used: 0, window: 0, sessionTokens: 0 } as ContextInfo;
  }
  const o = raw as Record<string, number>;
  return {
    used: o.used ?? 0,
    window: o.window ?? 0,
    sessionTokens: o.sessionTokens ?? o.used ?? 0,
  } as ContextInfo;
}

/** Full bot snapshot so sidebar/IM helpers never hit undefined.trim / undefined.length. */
function emptyBotSettings(): BotSettingsView {
  return {
    enabled: false,
    model: "",
    toolApprovalMode: "ask",
    maxSteps: 0,
    debounceMs: 0,
    queueMode: "",
    queueCap: 0,
    queueDrop: "",
    ignoreSelfMessages: true,
    selfUserIds: { qq: [], feishu: [], weixin: [] },
    control: {
      enabled: false,
      addr: "127.0.0.1:0",
      tokenEnv: "REASONIX_BOT_CONTROL_TOKEN",
    },
    pairing: {
      enabled: false,
      requestTtlMinutes: 60,
      maxPendingPerPlatform: 3,
    },
    routes: [],
    allowlist: {
      enabled: false,
      allowAll: false,
      qqUsers: [],
      feishuUsers: [],
      weixinUsers: [],
      qqApprovers: [],
      feishuApprovers: [],
      weixinApprovers: [],
      qqAdmins: [],
      feishuAdmins: [],
      weixinAdmins: [],
      qqGroups: [],
      feishuGroups: [],
      weixinGroups: [],
    },
    qq: {
      enabled: false,
      appId: "",
      appSecretEnv: "QQ_BOT_APP_SECRET",
      secretSet: false,
      sandbox: false,
      model: "",
      toolApprovalMode: "ask",
      workspaceRoot: "",
      access: {
        enabled: false,
        allowAll: false,
        pairingEnabled: false,
        users: [],
        groups: [],
        approvers: [],
        admins: [],
      },
    },
    feishu: {
      enabled: false,
      domain: "feishu",
      appId: "",
      appSecretEnv: "FEISHU_BOT_APP_SECRET",
      secretSet: false,
      verificationToken: "",
      mode: "webhook",
      webhookPort: 8080,
      requireMention: true,
    },
    weixin: {
      enabled: false,
      accountId: "",
      tokenEnv: "WEIXIN_BOT_TOKEN",
      tokenSet: false,
      apiBase: "",
    },
    connections: [],
  };
}

function emptyDesktopStartupSettings(): DesktopStartupSettingsView {
  return {
    bot: emptyBotSettings(),
    desktopLanguage: "",
    desktopLayoutStyle: "classic",
    desktopTheme: "auto",
    desktopThemeStyle: "",
    desktopTerminalTheme: "auto",
    displayMode: "standard",
    statusBarStyle: "icon",
    statusBarItems: [...DEFAULT_STATUS_BAR_ITEMS],
    checkUpdates: false,
    updateChannel: "stable",
    conversationWidth: "standard",
    configWarnings: [],
  };
}

/**
 * Full SettingsView stub for Electron PoC (settings panel may open even when
 * serve has no Wails settings surface). Every array field is a real array.
 */
function emptySettings(): SettingsView {
  const bot = emptyBotSettings();
  return {
    defaultModel: "",
    plannerModel: "",
    subagentModel: "",
    subagentEffort: "",
    autoPlan: "off",
    providers: [],
    officialProviders: [],
    providerPresets: [],
    permissions: { mode: "ask", allow: [], ask: [], deny: [] },
    sandbox: {
      bash: "workspace",
      network: true,
      workspaceRoot: "",
      allowWrite: [],
      effectiveWorkspaceRoot: "",
      effectiveWriteRoots: [],
      shell: "auto",
      effectiveShell: "",
    },
    network: {
      proxyMode: "auto",
      proxyUrl: "",
      noProxy: "",
      proxy: { type: "", server: "", port: 0, username: "", password: "" },
    },
    agent: {
      temperature: 0,
      maxSteps: 0,
      plannerMaxSteps: 0,
      maxSubagentDepth: 2,
      maxSubagentConcurrency: 1,
      maxParallelWriters: 1,
      systemPrompt: "",
      coldResumePrune: false,
      reasoningLanguage: "auto",
      compactRatio: 0.8,
    },
    bot,
    desktopLanguage: "",
    desktopLayoutStyle: "classic",
    desktopTheme: "auto",
    desktopThemeStyle: "",
    desktopTerminalTheme: "auto",
    closeBehavior: "background",
    displayMode: "standard",
    statusBarStyle: "icon",
    statusBarItems: [...DEFAULT_STATUS_BAR_ITEMS],
    defaultToolApprovalMode: "ask",
    checkUpdates: false,
    updateChannel: "stable",
    telemetry: false,
    metrics: false,
    configPath: "",
    providerKinds: [],
    autoApproveTools: false,
    bypass: false,
    conversationWidth: "standard",
  };
}

function emptyBotRuntimeStatus() {
  return {
    running: false,
    status: "stopped",
    message: "",
    connections: 0,
    startedAt: "",
  };
}

const ARRAY_METHOD_NAMES = new Set([
  "BackgroundRuntimes",
  "RemoteHosts",
  "RemoteConnectionStatuses",
  "ListProjectTree",
  "ListTabs",
  "ListSessions",
  "ListSessionsForTab",
  "ListTrashedSessions",
  "ListSkills",
  "ListWorkspaces",
  "ExtensionActions",
  "Checkpoints",
  "CheckpointsForTab",
  "Jobs",
  "JobsForTab",
  "Models",
  "History",
  "HistoryForTab",
  "HistoryCheckpointTurnsForTab",
]);

function looksLikeArrayMethod(key: string): boolean {
  if (ARRAY_METHOD_NAMES.has(key)) return true;
  if (key.startsWith("List")) return true;
  if (
    key.endsWith("s") &&
    (key.includes("Host") ||
      key.includes("Runtime") ||
      key.includes("Status") ||
      key.includes("Tree") ||
      key.includes("Topic") ||
      key.includes("Session") ||
      key.includes("Skill") ||
      key.includes("Job") ||
      key.includes("Warning") ||
      key.includes("Item") ||
      key.includes("Provider") ||
      key.includes("Model") ||
      key.includes("Checkpoint") ||
      key.includes("Action"))
  ) {
    return true;
  }
  return false;
}

/** Heuristic: methods that App always treats as arrays must never return undefined. */
function defaultForUnknownMethod(key: string): unknown {
  if (looksLikeArrayMethod(key)) return [];
  if (key.startsWith("Is") || key.startsWith("Has") || key.startsWith("Can") || key === "NeedsOnboarding") {
    return false;
  }
  if (key.includes("Count")) return 0;
  if (key === "DesktopStartupSettings" || key === "GetDesktopStartupSettings") {
    return emptyDesktopStartupSettings();
  }
  if (key === "Settings" || key === "GetSettings") {
    return emptySettings();
  }
  if (key === "BotRuntimeStatus") {
    return emptyBotRuntimeStatus();
  }
  if (key.startsWith("Get") || key.endsWith("View") || key.includes("Status")) {
    return null;
  }
  return undefined;
}

/**
 * Build AppBindings that drive multi-tab HttpSseHost.
 */
export function makeElectronHttpApp(
  host: HttpSseHost,
  endpoint: ElectronServeEndpoint,
): AppBindings {
  let multiTabAvailable: boolean | null = null;

  async function detectMultiTab(): Promise<boolean> {
    if (multiTabAvailable != null) return multiTabAvailable;
    try {
      await host.listTabs();
      multiTabAvailable = true;
    } catch {
      multiTabAvailable = false;
    }
    return multiTabAvailable;
  }

  async function listTabMetas(): Promise<TabMeta[]> {
    if (!(await detectMultiTab())) return [];
    const raw = await host.listTabs();
    if (!Array.isArray(raw)) return [];
    return raw.map(asTabMeta).filter((t): t is TabMeta => t != null);
  }

  async function activeTabId(): Promise<string | undefined> {
    const tabs = await listTabMetas();
    return tabs.find((t) => t.active)?.id ?? tabs[0]?.id;
  }

  async function resolveTabId(tabID?: unknown): Promise<string> {
    if (typeof tabID === "string" && tabID) return tabID;
    const id = await activeTabId();
    if (!id) throw new Error("no open tab");
    return id;
  }

  async function getTab(tabID?: unknown): Promise<TabMeta> {
    const id = await resolveTabId(tabID);
    const tabs = await listTabMetas();
    const found = tabs.find((t) => t.id === id);
    if (!found) throw new Error(`tab not found: ${id}`);
    return found;
  }

  const handlers: Record<string, (...args: unknown[]) => unknown> = {
    Platform: async () => {
      const p = navigator.platform || "";
      if (/Win/i.test(p)) return "windows";
      if (/Mac/i.test(p)) return "darwin";
      return "linux";
    },
    MinimiseMainWindow: async () => {},
    ToggleMaximiseMainWindow: async () => {},
    IsMainWindowMaximised: async () => false,
    CloseMainWindow: async () => {
      window.close();
    },

    ListTabs: async () => listTabMetas(),
    SetActiveTab: async (id: unknown) => {
      await host.activateTab(String(id));
    },
    EnsureBlankTab: async (scope: unknown, workspaceRoot: unknown) => {
      const root = String(workspaceRoot || endpoint.workspace || "");
      const meta = await host.createTab({
        scope: String(scope || "project"),
        workspaceRoot: root,
      });
      const t = asTabMeta(meta);
      if (!t) throw new Error("create tab failed");
      return t;
    },
    EnsureBlankSurface: async (scope: unknown, workspaceRoot: unknown) => {
      return handlers.EnsureBlankTab(scope, workspaceRoot);
    },
    OpenProjectTab: async (workspaceRoot: unknown, topicId?: unknown) => {
      const root = String(workspaceRoot || "");
      if (!root) throw new Error("workspaceRoot required");
      // Prefer an already-open tab for this workspace so UI can switch without
      // treating every sidebar click as a brand-new surface.
      const existing = (await listTabMetas()).find(
        (t) => t.scope === "project" && t.workspaceRoot === root,
      );
      if (existing) {
        await host.activateTab(existing.id);
        const refreshed = (await listTabMetas()).find((t) => t.id === existing.id);
        return refreshed ?? existing;
      }
      const meta = await host.openProject(root, String(topicId || "main"));
      const t = asTabMeta(meta);
      if (!t) throw new Error("open project failed");
      return t;
    },
    OpenGlobalTab: async () => {
      // Global scope: create tab with empty root if server allows; else reuse active.
      try {
        const meta = await host.createTab({ scope: "global", workspaceRoot: endpoint.workspace || "." });
        const t = asTabMeta(meta);
        if (t) return t;
      } catch {
        /* fallthrough */
      }
      return getTab();
    },
    OpenTopicSession: async (_scope: unknown, workspaceRoot: unknown, topicId: unknown) => {
      // Serve has no separate topic-session open; map to open-project / activate.
      const root = String(workspaceRoot || "");
      if (root) return handlers.OpenProjectTab(root, topicId);
      return getTab();
    },
    ActivateTopic: async (_scope: unknown, workspaceRoot: unknown, topicId: unknown, _sessionPath: unknown) => {
      const root = String(workspaceRoot || endpoint.workspace || "");
      return handlers.OpenProjectTab(root, topicId);
    },
    CloseTab: async (id: unknown) => {
      await host.closeTab(String(id));
    },
    CloseTabWithPolicy: async (id: unknown) => {
      await host.closeTab(String(id));
    },
    ReorderTabs: async () => {
      // Tab order is UI-local for serve multi-tab; no-op on host is fine.
    },
    CreateTopic: async (scope: unknown, workspaceRoot: unknown, title: unknown) => {
      const meta = await host.createTopic(String(scope || "project"), String(workspaceRoot || ""), String(title || ""));
      if (!meta || typeof meta !== "object") throw new Error("create topic failed");
      return meta;
    },
    RenameTopic: async (topicId: unknown, title: unknown) => {
      // Bridge only passes topicId+title; serve resolves workspace ownership.
      await host.renameTopic("", String(topicId || ""), String(title || ""));
    },
    DeleteTopic: async (topicId: unknown) => {
      await host.deleteTopic("", String(topicId || ""));
    },
    TrashTopic: async (topicId: unknown) => {
      await host.trashTopic("", String(topicId || ""));
    },
    RenameProject: async (workspaceRoot: unknown, title: unknown) => {
      await host.renameProject(String(workspaceRoot || ""), String(title || ""));
    },
    ReorderProjects: async (workspaceRoots: unknown) => {
      const roots = Array.isArray(workspaceRoots) ? workspaceRoots.map(String) : [];
      await host.reorderProjects(roots);
    },

    Submit: async (input: unknown) => {
      const id = await activeTabId();
      await host.submitPreferTab(id, String(input ?? ""));
    },
    SubmitToTab: async (tabID: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    SubmitDisplay: async (_display: unknown, input: unknown) => {
      const id = await activeTabId();
      await host.submitPreferTab(id, String(input ?? ""));
    },
    SubmitDisplayToTab: async (tabID: unknown, _display: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    SubmitDeliveryRecoveryToTab: async (tabID: unknown, _display: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    SubmitInvocationsToTab: async (tabID: unknown, _display: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    SubmitInitialGoalToTab: async (tabID: unknown, _display: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    SubmitEditedDisplayToTab: async (tabID: unknown, _display: unknown, input: unknown) => {
      await host.submitPreferTab(String(tabID), String(input ?? ""));
    },
    Cancel: async () => {
      const id = await activeTabId();
      if (id) await host.cancelTab(id).catch(() => host.cancel());
      else await host.cancel();
    },
    CancelTab: async (tabID: unknown) => {
      await host.cancelTab(String(tabID)).catch(() => host.cancel());
    },
    Approve: async (id: unknown, allow: unknown, session: unknown, persist: unknown) => {
      const tabId = await activeTabId();
      if (tabId) await host.approveTab(tabId, String(id), !!allow, !!session, !!persist);
      else await host.approve(String(id), !!allow, !!session, !!persist);
    },
    ApproveTab: async (tabID: unknown, id: unknown, allow: unknown, session: unknown, persist: unknown) => {
      await host.approveTab(String(tabID), String(id), !!allow, !!session, !!persist);
    },
    AnswerQuestion: async (id: unknown, answers: unknown) => {
      const tabId = await activeTabId();
      if (tabId) await host.answerTab(tabId, String(id), answers);
      else await host.answer(String(id), answers);
    },
    AnswerQuestionForTab: async (tabID: unknown, id: unknown, answers: unknown) => {
      await host.answerTab(String(tabID), String(id), answers);
    },
    ReplayPendingPrompts: async () => {
      // SSE attach triggers ReplayPendingPrompts server-side for multi-tab.
    },
    SetPlanMode: async (on: unknown) => {
      const id = await activeTabId();
      if (id) await host.setPlanModeTab(id, !!on);
      else await host.setPlanMode(!!on);
    },
    SetMode: async () => {},
    SetModeForTab: async () => [],
    SetAutoApproveTools: async (on: unknown) => {
      await host.setAutoApproveTools(!!on);
    },
    SetCollaborationMode: async () => {},
    SetCollaborationModeForTab: async () => {},
    SetToolApprovalMode: async (mode: unknown) => {
      const id = await activeTabId();
      if (id) await host.setToolApprovalModeTab(id, String(mode ?? "ask"));
      else await host.setToolApprovalMode(String(mode ?? "ask"));
    },
    SetToolApprovalModeForTab: async (tabID: unknown, mode: unknown) => {
      await host.setToolApprovalModeTab(String(tabID), String(mode ?? "ask"));
      return [];
    },
    SetComposerProfileForTab: async () => [],
    SetGoal: async (goal: unknown) => {
      const id = await activeTabId();
      if (id) await host.setGoalTab(id, String(goal ?? ""));
      else await host.setGoal(String(goal ?? ""));
    },
    SetGoalForTab: async (tabID: unknown, goal: unknown) => {
      await host.setGoalTab(String(tabID), String(goal ?? ""));
    },
    ClearGoal: async () => {
      const id = await activeTabId();
      if (id) await host.setGoalTab(id, "");
      else await host.clearGoal();
    },
    ClearGoalForTab: async (tabID: unknown) => {
      await host.setGoalTab(String(tabID), "");
    },
    ResumeGoalForTab: async () => false,
    PauseGoalForTab: async () => false,
    Compact: async () => {
      const id = await activeTabId();
      if (id) await host.compactTab(id);
      else await host.compact();
    },
    CompactForTab: async (tabID: unknown) => {
      await host.compactTab(String(tabID));
    },
    NewSession: async () => {
      const id = await activeTabId();
      if (id) await host.newSessionTab(id);
      else await host.newSession();
    },
    NewSessionForTab: async (tabID: unknown) => {
      await host.newSessionTab(String(tabID));
    },
    ClearSession: async () => handlers.NewSession(),
    ClearSessionForTab: async (tabID: unknown) => handlers.NewSessionForTab(tabID),
    History: async () => {
      const id = await activeTabId();
      const raw = id ? await host.historyTab(id) : await host.history();
      return Array.isArray(raw) ? raw : [];
    },
    HistoryForTab: async (tabID: unknown) => {
      const raw = await host.historyTab(String(tabID));
      return Array.isArray(raw) ? raw : [];
    },
    HistoryPage: async () => historyToPage(await handlers.History()),
    HistoryPageForTab: async (tabID: unknown) => historyToPage(await handlers.HistoryForTab(tabID)),
    HistoryCheckpointTurnsForTab: async () => [],
    Checkpoints: async () => {
      try {
        const cps = await host.checkpoints();
        return Array.isArray(cps) ? cps : [];
      } catch {
        return [];
      }
    },
    CheckpointsForTab: async () => handlers.Checkpoints(),
    Rewind: async (turn: unknown, scope: unknown) => {
      await host.rewind(Number(turn) || 0, String(scope || "both"));
    },
    RewindForTab: async (_tab: unknown, turn: unknown, scope: unknown) => {
      await host.rewind(Number(turn) || 0, String(scope || "both"));
    },
    Fork: async (turn: unknown) => {
      await host.fork(Number(turn) || 0);
      return getTab();
    },
    ForkForTab: async (tabID: unknown, turn: unknown) => {
      await host.fork(Number(turn) || 0);
      return getTab(tabID);
    },
    SummarizeFrom: async (turn: unknown) => {
      await host.summarize(Number(turn) || 0);
    },
    SummarizeFromForTab: async (_tab: unknown, turn: unknown) => {
      await host.summarize(Number(turn) || 0);
    },
    SummarizeUpTo: async (turn: unknown) => {
      await host.summarize(Number(turn) || 0);
    },
    SummarizeUpToForTab: async (_tab: unknown, turn: unknown) => {
      await host.summarize(Number(turn) || 0);
    },
    ListSessions: async () => {
      try {
        const s = await host.sessions();
        return Array.isArray(s) ? s : [];
      } catch {
        return [];
      }
    },
    ListSessionsForTab: async () => handlers.ListSessions(),
    ListTrashedSessions: async () => [],
    ResumeSession: async (path: unknown) => {
      await host.resume(String(path));
      return handlers.History();
    },
    ResumeSessionForTab: async (_tab: unknown, path: unknown) => {
      await host.resume(String(path));
      return handlers.HistoryForTab(_tab);
    },
    ResumeSessionPage: async (path: unknown) => {
      await host.resume(String(path));
      return historyToPage(await handlers.History());
    },
    ResumeSessionPageForTab: async (tabID: unknown, path: unknown) => {
      await host.resume(String(path));
      return historyToPage(await handlers.HistoryForTab(tabID));
    },
    DeleteSession: async (path: unknown) => {
      await host.deleteSession(String(path));
    },
    ListWorkspaces: async (): Promise<WorkspaceView[]> => {
      const tabs = await listTabMetas();
      const seen = new Map<string, WorkspaceView>();
      for (const t of tabs) {
        const p = t.workspaceRoot;
        if (!p || seen.has(p)) continue;
        seen.set(p, {
          path: p,
          name: t.workspaceName || p.split(/[/\\]/).filter(Boolean).pop() || p,
          current: !!t.active,
        });
      }
      return Array.from(seen.values());
    },
    PickWorkspace: async () => {
      const poc = (
        window as unknown as {
          reasonixPoc?: { pickWorkspace: () => Promise<{ workspace?: string } | null> };
        }
      ).reasonixPoc;
      if (!poc?.pickWorkspace) {
        throw new Error("Workspace picker is unavailable in this shell");
      }
      const res = await poc.pickWorkspace();
      const path = (res?.workspace || "").trim();
      if (!path) return ""; // user cancelled
      // Must open a Controller tab; otherwise the sidebar refresh still has no project.
      const meta = await host.openProject(path);
      if (!asTabMeta(meta)) {
        throw new Error(`Failed to open project: ${path}`);
      }
      return path;
    },
    SwitchWorkspace: async (path: unknown) => {
      const p = String(path ?? "").trim();
      if (!p) return "";
      const meta = await host.openProject(p);
      if (!asTabMeta(meta)) {
        throw new Error(`Failed to switch workspace: ${p}`);
      }
      return p;
    },
    Meta: async () => metaFromTab(await getTab()),
    MetaForTab: async (tabID: unknown) => metaFromTab(await getTab(tabID)),
    ContextUsage: async () => {
      const id = await activeTabId();
      if (!id) return { used: 0, window: 0, sessionTokens: 0 } as ContextInfo;
      return contextFromRaw(await host.contextTab(id).catch(() => host.context()));
    },
    ContextUsageForTab: async (tabID: unknown) => {
      return contextFromRaw(await host.contextTab(String(tabID)));
    },
    ContextPanel: async () => ({}),
    ContextPanelForTab: async () => ({}),
    Effort: async (): Promise<EffortInfo> => ({ level: "", options: [] } as unknown as EffortInfo),
    EffortForTab: async (): Promise<EffortInfo> => ({ level: "", options: [] } as unknown as EffortInfo),
    Jobs: async () => {
      try {
        const st = (await host.status()) as { jobs?: unknown };
        return Array.isArray(st?.jobs) ? st.jobs : [];
      } catch {
        return [];
      }
    },
    JobsForTab: async () => handlers.Jobs(),
    Balance: async () => {
      try {
        const st = (await host.status()) as { balance?: unknown };
        return st?.balance ?? null;
      } catch {
        return null;
      }
    },
    BalanceForTab: async () => handlers.Balance(),
    Models: async () => {
      try {
        const m = await host.models();
        return Array.isArray(m) ? m : [];
      } catch {
        return [];
      }
    },
    GetSkillsSettings: async () => ({ skills: [], roots: [] }),
    ListSkills: async () => {
      try {
        const s = await host.skills();
        return Array.isArray(s) ? s : [];
      } catch {
        return [];
      }
    },
    ListProjectTree: async () => {
      try {
        const tree = await host.projectTree();
        if (Array.isArray(tree) && tree.length > 0) return tree;
      } catch {
        /* fall back to open-tabs tree */
      }
      return projectTreeFromTabs(await listTabMetas());
    },
    BackgroundRuntimes: async () => [],
    RemoteHosts: async () => [],
    RemoteConnectionStatuses: async () => [],
    ExtensionActions: async () => [],
    ExternalOpeners: async () => ({ openers: [], preferred: "" }),
    NeedsOnboarding: async () => false,
    BotRuntimeStatus: async () => emptyBotRuntimeStatus(),
    DesktopStartupSettings: async () => {
      try {
        const raw = await host.desktopStartupSettings();
        if (raw && typeof raw === "object") {
          return { ...emptyDesktopStartupSettings(), ...(raw as object) };
        }
      } catch {
        /* fall through */
      }
      return emptyDesktopStartupSettings();
    },
    GetDesktopStartupSettings: async () => handlers.DesktopStartupSettings(),
    Settings: async () => {
      try {
        const raw = await host.desktopSettings();
        if (raw && typeof raw === "object") {
          return { ...emptySettings(), ...(raw as object) };
        }
      } catch {
        /* fall through */
      }
      return emptySettings();
    },
    GetSettings: async () => handlers.Settings(),
    ReloadUserConfig: async () => handlers.Settings(),
    MigrateDesktopPreferences: async () => {},
    SetTrayLocale: async () => {},
    GetActiveThemePack: async () => null,
    RemoveWorkspace: async (path: unknown) => {
      await host.removeProject(String(path || ""));
    },
    ReloadExtensions: async () => {
      await host.reloadExtensions().catch(() => {});
    },
    ResolvePlanDecision: async () => {},
    ResolvePlanDecisionTab: async () => {},
    ResolveRecovery: async () => {},
    ResolveRecoveryTab: async () => {},
    ActiveWorkForTab: async () => ({ running: false, pendingPrompt: false, jobs: [] }),
    WorkspaceConflictForTab: async () => null,
    RunShell: async () => {
      throw new Error("Shell is unavailable over HTTP serve (Electron multi-tab)");
    },
    RunShellForTab: async () => {
      throw new Error("Shell is unavailable over HTTP serve (Electron multi-tab)");
    },
    Steer: async () => {
      throw new Error("Steer is not exposed on reasonix serve");
    },
    SteerForTab: async () => {
      throw new Error("Steer is not exposed on reasonix serve");
    },
  };

  (handlers as Record<string, unknown>).__pocCapabilities = {
    ...POC_CAPABILITIES,
    multiTab: true,
    singleSession: false,
  };
  (handlers as Record<string, unknown>).__httpHost = host;

  return new Proxy({} as AppBindings, {
    get(_t, prop) {
      const key = String(prop);
      if (key in handlers) return handlers[key];
      return async (..._args: unknown[]) => defaultForUnknownMethod(key);
    },
  });
}

export type ElectronHttpAppBundle = {
  app: AppBindings;
  host: HttpSseHost;
  endpoint: ElectronServeEndpoint;
};

export function createElectronHttpAppBundle(endpoint: ElectronServeEndpoint): ElectronHttpAppBundle {
  const host = new HttpSseHost({
    baseUrl: endpoint.baseUrl,
    token: endpoint.token,
    capabilities: { ...POC_CAPABILITIES, multiTab: true, singleSession: false },
  });
  return {
    app: makeElectronHttpApp(host, endpoint),
    host,
    endpoint,
  };
}

// Deprecated alias kept so older imports of SERVE_TAB_ID still typecheck if any remain.
export const SERVE_TAB_ID = "";
