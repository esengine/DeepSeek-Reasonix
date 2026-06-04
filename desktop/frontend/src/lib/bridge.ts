// bridge is the single seam between the React app and the Go kernel. In the Wails
// shell it calls the bound App methods (window.go.main.App.*) and subscribes to
// the runtime event stream (window.runtime.EventsOn). In a plain browser (`pnpm
// dev` outside the shell) those globals are absent, so it falls back to a mock
// that streams a canned turn through the same contract — letting the whole UI be
// developed and laid out without rebuilding the Go side.

import type {
  BalanceInfo,
  CapabilitiesView,
  CheckpointMeta,
  CommandInfo,
  ContextInfo,
  ContextPanelInfo,
  DirEntry,
  DroppedItem,
  EffortInfo,
  FilePreview,
  HistoryMessage,
  JobView,
  MCPServerInput,
  MemoryView,
  Meta,
  ModelInfo,
  NetworkView,
  ProjectNode,
  ProviderView,
  QuestionAnswer,
  ServerView,
  SessionMeta,
  SettingsView,
  SkillRootView,
  SkillView,
  SlashArgsResult,
  TabMeta,
  TopicMeta,
  UpdateInfo,
  UpdateProgress,
  WireEvent,
  WorkspaceChangesView,
  WorkspaceView,
} from "./types";

// AppBindings mirrors desktop/app.go's exported method set. Keep in sync by hand
// (or regenerate with `wails generate module` and import wailsjs instead).
export interface AppBindings {
  Platform(): Promise<string>;
  Submit(input: string): Promise<void>;
  SubmitDisplay(display: string, input: string): Promise<void>;
  Cancel(): Promise<void>;
  Approve(id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  AnswerQuestion(id: string, answers: QuestionAnswer[]): Promise<void>;
  SetPlanMode(on: boolean): Promise<void>;
  // SetMode applies plan/yolo/normal gating atomically (one IPC, no half-applied
  // window); prefer it over sequencing SetPlanMode + SetBypass from the UI.
  SetMode(mode: string): Promise<void>;
  Compact(): Promise<void>;
  NewSession(): Promise<void>;
  History(): Promise<HistoryMessage[]>;
  // Checkpoints lists the session's rewind points; Rewind restores one (scope
  // "code" | "conversation" | "both"), after which the caller re-reads History.
  Checkpoints(): Promise<CheckpointMeta[]>;
  Rewind(turn: number, scope: string): Promise<void>;
  Fork(turn: number): Promise<void>;
  SummarizeFrom(turn: number): Promise<void>;
  SummarizeUpTo(turn: number): Promise<void>;
  // Session history: list saved sessions, resume one (returns its transcript),
  // preview one read-only, delete one, or give one a custom display name ("" clears it).
  ListSessions(): Promise<SessionMeta[]>;
  ResumeSession(path: string): Promise<HistoryMessage[]>;
  PreviewSession(path: string): Promise<HistoryMessage[]>;
  DeleteSession(path: string): Promise<void>;
  RenameSession(path: string, title: string): Promise<void>;
  // Workspace: open a folder chooser and switch to that project (fresh session);
  // returns the chosen path, or "" if cancelled.
  ListWorkspaces(): Promise<WorkspaceView[]>;
  PickWorkspace(): Promise<string>;
  SwitchWorkspace(path: string): Promise<string>;
  ContextUsage(): Promise<ContextInfo>;
  // Balance queries the active provider's wallet balance (a network call);
  // returns an unavailable readout when no balance_url is configured or it fails.
  Balance(): Promise<BalanceInfo>;
  // Jobs lists the running background jobs (bash/task started in the background)
  // for the status-bar indicator.
  Jobs(): Promise<JobView[]>;
  Meta(): Promise<Meta>;
  Commands(): Promise<CommandInfo[]>;
  // Capabilities feeds the MCP & Skills drawer: connected/failed servers + skills.
  // Add connects + persists a server; Remove disconnects + drops it from config;
  // Retry reconnects a configured server that failed (config untouched).
  Capabilities(): Promise<CapabilitiesView>;
  AddMCPServer(input: MCPServerInput): Promise<number>;
  UpdateMCPServer(name: string, input: MCPServerInput): Promise<void>;
  RemoveMCPServer(name: string): Promise<void>;
  RetryMCPServer(name: string): Promise<void>;
  ClearMCPServerAuthentication(name: string): Promise<void>;
  PickSkillFolder(): Promise<string>;
  AddSkillPath(path: string): Promise<void>;
  RemoveSkillPath(path: string): Promise<void>;
  RefreshSkills(): Promise<void>;
  SetSkillEnabled(name: string, enabled: boolean): Promise<void>;
  // SetMCPServerEnabled is the per-session connector toggle (on reconnects, off
  // disconnects; config untouched).
  SetMCPServerEnabled(name: string, enabled: boolean): Promise<void>;
  SetMCPServerTier(name: string, tier: string): Promise<void>;
  SlashArgs(input: string): Promise<SlashArgsResult>;
  ListDir(rel: string): Promise<DirEntry[]>;
  SearchFileRefs(query: string): Promise<DirEntry[]>;
  ReadFile(rel: string): Promise<FilePreview>;
  WorkspaceChanges(): Promise<WorkspaceChangesView>;
  OpenWorkspacePath(rel: string): Promise<void>;
  RevealWorkspacePath(rel: string): Promise<void>;
  SavePastedImage(dataUrl: string): Promise<string>;
  SavePastedFile(name: string, dataUrl: string): Promise<string>;
  // AttachDropped resolves an OS-dropped absolute path (from the native file-drop
  // bridge) into a composer context entry — a workspace ref or a stored attachment.
  AttachDropped(path: string): Promise<DroppedItem>;
  AttachmentDataURL(path: string): Promise<string>;
  Models(): Promise<ModelInfo[]>;
  SetModel(name: string): Promise<void>;
  Effort(): Promise<EffortInfo>;
  SetEffort(level: string): Promise<void>;
  // Memory panel: read the loaded REASONIX.md hierarchy + saved auto-memories,
  // quick-add a note to a scope's REASONIX.md (≡ "#<note>"), and overwrite a doc
  // from the in-place editor.
  Memory(): Promise<MemoryView>;
  Remember(scope: string, note: string): Promise<string>;
  Forget(name: string): Promise<void>;
  SaveDoc(path: string, body: string): Promise<string>;
  // Settings panel: read the resolved config and apply edits (each writes config
  // and rebuilds the controller live). Secrets go through SetProviderKey (→ .env).
  Settings(): Promise<SettingsView>;
  SetDefaultModel(ref: string): Promise<void>;
  SetPlannerModel(ref: string): Promise<void>;
  SaveProvider(p: ProviderView): Promise<void>;
  DeleteProvider(name: string): Promise<void>;
  SetProviderKey(apiKeyEnv: string, value: string): Promise<void>;
  SetPermissionMode(mode: string): Promise<void>;
  AddPermissionRule(list: string, rule: string): Promise<void>;
  RemovePermissionRule(list: string, rule: string): Promise<void>;
  SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]): Promise<void>;
  SetNetwork(n: NetworkView): Promise<void>;
  SetAgentParams(temperature: number, maxSteps: number, systemPrompt: string): Promise<void>;
  // SetBypass toggles YOLO mode (auto-approve every tool call this session; deny
  // rules still apply). Runtime-only — not written to config.
  SetBypass(on: boolean): Promise<void>;
  // Auto-updater (desktop/updater_app.go): the injected build version, a manifest
  // check, applying an update (win/linux self-update; macOS opens the download
  // page), and opening that page directly. Progress streams on "updater:progress".
  Version(): Promise<string>;
  CheckUpdate(): Promise<UpdateInfo | null>;
  ApplyUpdate(): Promise<void>;
  OpenDownloadPage(): Promise<void>;
  // First-run overlay: NeedsOnboarding is true when the default provider key is
  // unset; ConnectKey validates, persists to ./.env, and rebuilds the controller.
  NeedsOnboarding(): Promise<boolean>;
  ConnectKey(apiKey: string): Promise<void>;
  // Tab management (desktop/tabs.go).
  ListTabs(): Promise<TabMeta[]>;
  OpenProjectTab(workspaceRoot: string, topicID: string): Promise<TabMeta>;
  OpenGlobalTab(topicID: string): Promise<TabMeta>;
  SetActiveTab(tabID: string): Promise<void>;
  CloseTab(tabID: string): Promise<void>;
  // Project tree (desktop/tabs.go).
  ListProjectTree(): Promise<ProjectNode[]>;
  CreateTopic(scope: string, workspaceRoot: string, title: string): Promise<TopicMeta>;
  RenameTopic(topicID: string, title: string): Promise<void>;
  DeleteTopic(topicID: string): Promise<void>;
  // Context panel (desktop/tabs.go).
  ContextPanel(tabID: string): Promise<ContextPanelInfo>;
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
  BrowserOpenURL(url: string): void;
  // Native OS file drop (desktop only); useDropTarget gates delivery to elements
  // carrying the --wails-drop-target CSS property. Absent in the browser dev mock.
  OnFileDrop?(cb: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
  OnFileDropOff?(): void;
}

declare global {
  interface Window {
    runtime?: WailsRuntime;
    go?: { main?: { App?: AppBindings } };
  }
}

// Must match desktop/app.go's eventChannel constant.
const EVENT_CHANNEL = "agent:event";

// Resolve the Wails binding at CALL time, not module-load time: in dev the Wails
// runtime can inject window.go AFTER this module first evaluates, so snapshotting
// once would pin the browser mock for the whole session (and show fake data — the
// dev mock's model list leaking into the real app was exactly this bug).
function realApp(): AppBindings | undefined {
  return typeof window !== "undefined" ? window.go?.main?.App : undefined;
}

let mockSingleton: AppBindings | null = null;
function getMock(): AppBindings {
  if (!mockSingleton) mockSingleton = makeMockApp();
  return mockSingleton;
}

// onEvent subscribes to the agent's typed event stream; returns an unsubscribe.
export function onEvent(cb: (e: WireEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(EVENT_CHANNEL, (payload) => cb(payload as WireEvent));
  }
  return mockSubscribe(cb);
}

// onUpdaterProgress subscribes to the auto-updater's progress events (a separate
// channel from the agent stream); returns an unsubscribe. Must match the event
// name emitted in desktop/updater_app.go.
export function onUpdaterProgress(cb: (p: UpdateProgress) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("updater:progress", (p) => cb(p as UpdateProgress));
  }
  updaterListeners.add(cb);
  return () => {
    updaterListeners.delete(cb);
  };
}

// onFilesDropped subscribes to native OS file drops landing on the composer (the
// --wails-drop-target element); the callback gets the dropped files' absolute
// paths. No-op in the browser dev mock, where the runtime is absent.
export function onFilesDropped(cb: (paths: string[]) => void): () => void {
  const rt = typeof window !== "undefined" ? window.runtime : undefined;
  if (!rt?.OnFileDrop) return () => {};
  rt.OnFileDrop((_x, _y, paths) => {
    if (Array.isArray(paths) && paths.length > 0) cb(paths);
  }, true);
  return () => rt.OnFileDropOff?.();
}

// onReady subscribes to the agent:ready event fired when boot.Build completes.
// The frontend re-fetches Meta/Context/History when this lands.
export function onReady(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("agent:ready", () => cb());
  }
  // In dev mock, fire immediately since there's no real boot sequence.
  cb();
  return () => {};
}

// app proxies each call to the live binding (or the dev mock only when truly
// outside the shell), so a late-injected window.go is picked up transparently.
export const app: AppBindings = new Proxy({} as AppBindings, {
  get(_t, prop) {
    const target = realApp() ?? getMock();
    const v = (target as unknown as Record<string, unknown>)[String(prop)];
    return typeof v === "function" ? (v as (...a: unknown[]) => unknown).bind(target) : v;
  },
});

// openExternal opens a URL in the system browser (so links in rendered markdown
// don't navigate the webview away from the app). Falls back to window.open in the
// browser dev mock.
export function openExternal(url: string): void {
  if (typeof window !== "undefined" && window.runtime?.BrowserOpenURL) {
    window.runtime.BrowserOpenURL(url);
  } else if (typeof window !== "undefined") {
    window.open(url, "_blank", "noopener");
  }
}

// --- browser dev mock --------------------------------------------------------

const listeners = new Set<(e: WireEvent) => void>();

function mockSubscribe(cb: (e: WireEvent) => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

function emit(e: WireEvent) {
  listeners.forEach((l) => l(e));
}

// Updater progress has its own listener set so the browser dev mock's ApplyUpdate
// can stream a fake download through onUpdaterProgress.
const updaterListeners = new Set<(p: UpdateProgress) => void>();

function emitUpdater(p: UpdateProgress) {
  updaterListeners.forEach((l) => l(p));
}

function delay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function makeMockApp(): AppBindings {
  let cancelled = false;
  let pendingAskPreview = false;
  let pendingApprovalPreview = false;
  let cwd = "~/projects/reasonix"; // mutable so PickWorkspace is visible in dev
  let workspaces = ["~/projects/reasonix", "~/projects/blade", "~/projects/deepseek-forge", "~/projects/cc-switch-light", "~/projects/SuperRig"];
  let mockEffort = "auto";
  const day = 86_400_000;
  const t0 = Date.now();
  // Mutable so MCP add/remove/retry are observable in browser dev.
  let capServers: ServerView[] = [
    {
      name: "codegraph",
      transport: "stdio",
      status: "disabled",
      builtIn: true,
      configured: true,
      autoStart: false,
      tier: "lazy",
      tools: 0,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "search", description: "Search symbols, files, and text in the workspace." },
        { name: "context", description: "Fetch surrounding source context for a symbol or file." },
        { name: "trace", description: "Follow callers and callees across the code graph." },
        { name: "node", description: "Inspect a specific graph node." },
      ],
    },
    { name: "github", transport: "stdio", status: "connected", configured: true, autoStart: true, tier: "lazy", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"], tools: 12, prompts: 2, resources: 0 },
    {
      name: "linear",
      transport: "http",
      status: "deferred",
      configured: true,
      autoStart: true,
      tier: "lazy",
      url: "https://mcp.linear.app/mcp",
      authStatus: "possible",
      authUrl: "https://mcp.linear.app/mcp",
      tools: 8,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "list_issues", description: "List and filter Linear issues." },
        { name: "get_issue", description: "Fetch a Linear issue by id or key." },
        { name: "create_issue", description: "Create a Linear issue." },
        { name: "update_issue", description: "Update status, assignee, priority, or labels." },
        { name: "list_projects", description: "List Linear projects." },
        { name: "get_project", description: "Fetch project details." },
        { name: "list_teams", description: "List Linear teams." },
        { name: "search", description: "Search Linear workspace objects." },
      ],
    },
    { name: "figma", transport: "http", status: "failed", configured: true, autoStart: true, tier: "lazy", url: "https://mcp.figma.com/mcp", authStatus: "required", authUrl: "https://mcp.figma.com/mcp", tools: 0, prompts: 0, resources: 0, error: "connect: 401 unauthorized" },
  ];
  const capSkills: SkillView[] = [
    { name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "builtin", runAs: "subagent", enabled: true },
    { name: "review", description: "Review the staged diff", scope: "project", runAs: "inline", enabled: false },
    { name: "init", description: "Scaffold a REASONIX.md for this repo", scope: "builtin", runAs: "inline", enabled: true },
  ];
  let capSkillRoots: SkillRootView[] = [
    { dir: "~/projects/reasonix/.reasonix/skills", scope: "project", priority: 1, status: "missing", configured: false, skills: 0 },
    {
      dir: "~/my-skills",
      scope: "custom",
      priority: 5,
      status: "ok",
      configured: true,
      skills: 1,
      skillItems: [{ name: "review", description: "Review the staged diff", scope: "custom", runAs: "inline" }],
    },
    {
      dir: "~/.reasonix/skills",
      scope: "global",
      priority: 6,
      status: "ok",
      configured: false,
      skills: 2,
      skillItems: [
        { name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "global", runAs: "subagent" },
        { name: "init", description: "Scaffold a REASONIX.md for this repo", scope: "global", runAs: "inline" },
      ],
    },
  ];
  const mockSwitchWorkspace = async (path: string) => {
    cwd = path || "~";
    workspaces = [cwd, ...workspaces.filter((p) => p !== cwd)].slice(0, 12);
    return cwd;
  };
  // Mutable so delete/rename are observable in browser dev.
  const sessions: SessionMeta[] = [
    { path: "/mock/sessions/a.jsonl", preview: "fix the login bug in auth.go", turns: 12, createdAt: t0 - 2 * day, lastActivityAt: t0 - 3_600_000, modTime: t0 - 3_600_000, current: true },
    { path: "/mock/sessions/b.jsonl", preview: "refactor the payment module", turns: 5, createdAt: t0 - 3 * day, lastActivityAt: t0 - 6 * 3_600_000, modTime: t0 - 6 * 3_600_000, current: false },
    { path: "/mock/sessions/c.jsonl", preview: "write the README and badges", turns: 8, createdAt: t0 - 4 * day, lastActivityAt: t0 - day - 3_600_000, modTime: t0 - day - 3_600_000, current: false },
    { path: "/mock/sessions/d.jsonl", preview: "explain the plugin host design", turns: 3, createdAt: t0 - 5 * day, lastActivityAt: t0 - 4 * day, modTime: t0 - 4 * day, current: false },
  ];
  // Mutable settings so the Settings panel's edits are observable in browser dev.
  const settings: SettingsView = {
    defaultModel: "deepseek-flash",
    plannerModel: "",
    providers: [
      { name: "deepseek-flash", kind: "openai", baseUrl: "https://api.deepseek.com", models: ["deepseek-v4-flash"], default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: true, balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000 },
      { name: "mimo-pro", kind: "openai", baseUrl: "https://api.xiaomimimo.com/v1", models: ["mimo-v2.5-pro"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", keySet: false, balanceUrl: "", contextWindow: 1_000_000 },
    ],
    permissions: { mode: "ask", allow: ["ls", "read_file"], ask: [], deny: ["bash(rm *)"] },
    sandbox: { bash: "enforce", network: true, workspaceRoot: "", allowWrite: [] },
    network: {
      proxyMode: "auto",
      proxyUrl: "",
      noProxy: "",
      proxy: { type: "socks5", server: "127.0.0.1", port: 7890, username: "", password: "" },
    },
    agent: { temperature: 0.2, maxSteps: 0, systemPrompt: "You are Reasonix, a coding agent." },
    configPath: "~/projects/reasonix/reasonix.toml",
    providerKinds: ["openai"],
    bypass: false,
  };
  return {
    async Platform() {
      return "browser";
    },
    async Submit(input) {
      cancelled = false;
      emit({ kind: "turn_started" });
      const trimmedInput = input.trim().toLowerCase();
      if (trimmedInput === "/approve-preview" || trimmedInput === "approve preview" || trimmedInput === "approve预览") {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-approval-preview",
            tool: "bash",
            subject: "npm run build\n\n需要运行构建命令来验证前端产物和样式打包是否正常。",
          },
        });
        return;
      }
      if (
        trimmedInput === "/plan-approve-preview" ||
        trimmedInput === "plan approve preview" ||
        trimmedInput === "plan approve预览"
      ) {
        pendingApprovalPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-plan-approval-preview",
            tool: "exit_plan_mode",
            subject: "",
          },
        });
        return;
      }
      if (trimmedInput === "/ask-preview" || trimmedInput === "ask preview" || trimmedInput === "ask预览") {
        pendingAskPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "ask_request",
          ask: {
            id: "mock-ask-preview",
            questions: [
              {
                id: "q1",
                header: "处理方向",
                prompt: "git pull 的冲突你想怎么处理？",
                options: [
                  { label: "git stash 后 pull", description: "用 git stash 暂存本地修改，拉取最新代码后再恢复" },
                  { label: "丢弃本地修改后 pull", description: "放弃本地所有修改，强制与远端同步" },
                  { label: "另建分支保存改动", description: "先创建分支把本地改动保存起来，再拉取主分支" },
                ],
              },
              {
                id: "q2",
                header: "Reasonix 构建",
                prompt: "对于 reasonix 二进制缺失的问题，你想怎么做？",
                options: [
                  { label: "先查文档", description: "查看 README / 构建文档来确定正确的构建命令" },
                  { label: "看构建配置", description: "查看 desktop/wails.json 与 main.go 来推断入口" },
                  { label: "我先帮你尝试构建", description: "先处理 git 冲突，然后尝试本地构建并汇报结果" },
                ],
              },
            ],
          },
        });
        return;
      }
      // Simulate the server's pre-first-token latency so the deferred user bubble
      // and the "un-send on Esc before any reply" path are observable in browser
      // dev. Bail if cancelled during the wait — nothing was streamed yet.
      await delay(700);
      if (cancelled) return;
      const reply =
        `You said: **${input}**\n\n` +
        "This is the browser dev mock — the real reply comes from the kernel " +
        "inside the Wails shell. Here's a fenced block to exercise the editor seam:\n\n" +
        "```go\nfunc main() {\n    println(\"hello from the mock\")\n}\n```\n";
      for (const ch of reply) {
        if (cancelled) break;
        emit({ kind: "text", text: ch });
        await delay(6);
      }
      emit({ kind: "message", text: reply });
      emit({
        kind: "tool_dispatch",
        tool: {
          id: "t1",
          name: "edit_file",
          args: '{"path":"main.go","old_string":"println(\\"hi\\")","new_string":"println(\\"hello\\")"}',
          readOnly: false,
        },
      });
      await delay(350);
      emit({
        kind: "tool_result",
        tool: { id: "t1", name: "edit_file", output: "edited main.go", readOnly: false },
      });
      emit({
        kind: "usage",
        usage: {
          promptTokens: 1280,
          completionTokens: 64,
          totalTokens: 1344,
          cacheHitTokens: 1024,
          cacheMissTokens: 256,
          sessionCacheHitTokens: 1024,
          sessionCacheMissTokens: 256,
        },
      });
      emit({ kind: "turn_done" });
    },
    async SubmitDisplay(_display, input) {
      await this.Submit(input);
    },
    async Cancel() {
      cancelled = true;
      emit({ kind: "turn_done" });
    },
    async Approve(_id, allow, session, persist) {
      if (!pendingApprovalPreview) return;
      pendingApprovalPreview = false;
      const suffix = persist ? "persisted" : session ? "allowed for session" : "allowed once";
      emit({
        kind: "message",
        text: `approval preview answered: ${allow ? suffix : "denied"}`,
      });
      emit({ kind: "turn_done" });
    },
    async AnswerQuestion(_id, answers) {
      if (!pendingAskPreview) return;
      pendingAskPreview = false;
      const summary = answers
        .map((answer) => `${answer.questionId}: ${(answer.selected ?? []).join(", ") || "(no answer)"}`)
        .join("\n");
      emit({ kind: "message", text: `ask preview answered:\n\n${summary}` });
      emit({ kind: "turn_done" });
    },
    async SetPlanMode() {},
    async SetMode() {},
    async Compact() {},
    async NewSession() {},
    async Checkpoints() {
      return [];
    },
    async Rewind() {},
    async Fork() {},
    async SummarizeFrom() {},
    async SummarizeUpTo() {},
    async History() {
      return [];
    },
    async ListSessions() {
      return sessions.map((s) => ({ ...s }));
    },
    async ResumeSession(path: string) {
      sessions.forEach((s) => {
        s.current = s.path === path;
      });
      return [
        { role: "user", content: `(mock) resumed ${path}` },
        { role: "assistant", content: "This is a mock resumed transcript — the real one comes from the kernel." },
      ];
    },
    async PreviewSession(path: string) {
      const s = sessions.find((x) => x.path === path);
      return [
        { role: "user", content: s?.preview || `(mock) preview ${path}` },
        {
          role: "assistant",
          content: "This is a read-only mock preview. The active conversation is unchanged.",
          reasoning: "Preview reads the saved session without resuming it.",
        },
      ];
    },
    async DeleteSession(path: string) {
      const i = sessions.findIndex((s) => s.path === path);
      if (i >= 0) sessions.splice(i, 1);
    },
    async RenameSession(path: string, title: string) {
      const s = sessions.find((x) => x.path === path);
      if (s) s.title = title.trim() || undefined;
    },
    async ListWorkspaces() {
      return workspaces.map((path) => ({
        path,
        name: path.split("/").filter(Boolean).pop() ?? path,
        current: path === cwd,
      }));
    },
    async PickWorkspace() {
      // Browser dev has no native dialog; simulate picking a folder and re-root so
      // the topbar folder chip visibly changes.
      return mockSwitchWorkspace(cwd.endsWith("another-project") ? "~/projects/reasonix" : "~/projects/another-project");
    },
    async SwitchWorkspace(path: string) {
      return mockSwitchWorkspace(path);
    },
    async ContextUsage() {
      return { used: 1280, window: 1_000_000 };
    },
    async Balance() {
      // Mirror the active mock provider: deepseek-flash carries a balance_url.
      const p = settings.providers.find((x) => x.name === settings.defaultModel);
      if (!p?.balanceUrl) return { available: false, display: "" };
      return { available: true, display: "¥128.50" };
    },
    async Jobs() {
      return []; // browser dev mock has no background jobs
    },
    async Meta() {
      return {
        label: "mock model · browser dev",
        ready: true,
        eventChannel: EVENT_CHANNEL,
        cwd,
        bypass: settings.bypass,
      };
    },
    async Commands() {
      return [
        { name: "new", description: "Start a new session", kind: "builtin" as const },
        { name: "compact", description: "Summarize older history to free up context", kind: "builtin" as const },
        { name: "model", description: "Switch model", kind: "builtin" as const },
        { name: "effort", description: "Set reasoning effort", kind: "builtin" as const },
        { name: "skill", description: "List skills", kind: "builtin" as const },
        { name: "explore", description: "Investigate the codebase in an isolated subagent", kind: "skill" as const },
        { name: "review", description: "Review the staged diff", hint: "[focus]", kind: "custom" as const },
      ];
    },
    async Capabilities() {
      return {
        servers: capServers.map((s) => ({ ...s })),
        skills: capSkills.map((s) => ({ ...s })),
        skillRoots: capSkillRoots.map((s) => ({ ...s })),
      };
    },
    async AddMCPServer(input: MCPServerInput) {
      const tools = input.transport === "stdio" ? 3 : 5;
      capServers.push({
        name: input.name,
        transport: input.transport,
        status: "connected",
        configured: true,
        autoStart: true,
        tier: input.tier || "lazy",
        command: input.command,
        args: input.args,
        url: input.url,
        tools,
        prompts: 0,
        resources: 0,
        toolList: Array.from({ length: tools }, (_, i) => ({
          name: `${input.name}_tool_${i + 1}`,
          description: `Mock tool ${i + 1} exposed by ${input.name}.`,
        })),
      });
      return tools;
    },
    async UpdateMCPServer(name: string, input: MCPServerInput) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        const connected = s.status === "connected" || s.status === "failed" || input.tier !== "lazy";
        const nextStatus = s.status === "disabled" ? "disabled" : connected ? "connected" : "deferred";
        const nextTools = nextStatus === "connected" ? s.tools || (input.transport === "stdio" ? 3 : 5) : 0;
        return {
          ...s,
          transport: input.transport,
          status: nextStatus,
          tier: input.tier || "lazy",
          command: input.transport === "stdio" ? input.command : "",
          args: input.transport === "stdio" ? input.args : [],
          url: input.transport === "stdio" ? "" : input.url,
          envKeys: input.env ? Object.keys(input.env).sort() : s.envKeys,
          tools: nextTools,
          error: undefined,
          authStatus: nextStatus !== "connected" && input.transport !== "stdio" ? "possible" : undefined,
          authUrl: nextStatus !== "connected" && input.transport !== "stdio" ? input.url : undefined,
        };
      });
    },
    async RemoveMCPServer(name: string) {
      capServers = capServers.filter((s) => s.name !== name);
    },
    async RetryMCPServer(name: string) {
      capServers = capServers.map((s) =>
        s.name === name ? { ...s, status: "connected", tools: s.tools || 4, error: undefined, authStatus: undefined, authUrl: undefined } : s,
      );
    },
    async ClearMCPServerAuthentication(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: s.tier === "background" || s.tier === "eager" ? "initializing" : "deferred",
              tools: 0,
              error: undefined,
              authStatus: s.transport !== "stdio" ? "possible" : undefined,
              authUrl: s.transport !== "stdio" ? s.url : undefined,
              authConfigured: undefined,
            }
          : s,
      );
    },
    async PickSkillFolder() {
      return "~/my-skills";
    },
    async AddSkillPath(path: string) {
      const dir = path.trim() || "~/my-skills";
      if (!capSkillRoots.some((r) => r.scope === "custom" && r.dir === dir)) {
        capSkillRoots.push({
          dir,
          scope: "custom",
          priority: capSkillRoots.length + 1,
          status: "ok",
          configured: true,
          skills: 1,
          skillItems: [{ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline" }],
        });
      }
      if (!capSkills.some((s) => s.name === "local-dev")) {
        capSkills.push({ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline", enabled: true });
      }
    },
    async RemoveSkillPath(path: string) {
      capSkillRoots = capSkillRoots.filter((r) => !(r.scope === "custom" && r.dir === path));
      if (!capSkillRoots.some((r) => r.scope === "custom")) {
        const idx = capSkills.findIndex((s) => s.name === "local-dev");
        if (idx >= 0) capSkills.splice(idx, 1);
      }
    },
    async RefreshSkills() {},
    async SetSkillEnabled(name: string, enabled: boolean) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.enabled = enabled;
    },
    async SetMCPServerEnabled(name: string, enabled: boolean) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: enabled ? "connected" : "disabled",
              autoStart: s.builtIn ? enabled : s.autoStart,
              tools: enabled ? s.tools || 4 : 0,
              error: undefined,
              authStatus: !enabled && s.transport !== "stdio" ? "possible" : undefined,
              authUrl: !enabled && s.transport !== "stdio" ? s.url : undefined,
            }
          : s,
      );
    },
    async SetMCPServerTier(name: string, tier: string) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        if (tier === "lazy") return { ...s, tier, autoStart: true };
        const tools = s.tools || (s.transport === "stdio" ? 3 : 5);
        return { ...s, tier, autoStart: true, status: "connected", tools, error: undefined, authStatus: undefined, authUrl: undefined };
      });
    },
    async SlashArgs(input: string) {
      // Mirror a slice of the real arg hints so the menu is exercisable in browser dev.
      const from = input.lastIndexOf(" ") + 1;
      const cur = input.slice(from);
      const cmd = input.slice(0, input.indexOf(" ") < 0 ? input.length : input.indexOf(" "));
      const subs: Record<string, { label: string; insert: string; hint: string; descend?: boolean }[]> = {
        "/skill": [
          { label: "list", insert: "list", hint: "list skills" },
          { label: "show", insert: "show ", hint: "show a skill's body", descend: true },
          { label: "enable", insert: "enable ", hint: "enable a disabled skill", descend: true },
          { label: "disable", insert: "disable ", hint: "disable an enabled skill", descend: true },
          { label: "new", insert: "new ", hint: "scaffold a new skill" },
          { label: "paths", insert: "paths", hint: "show discovery paths" },
        ],
        "/hooks": [
          { label: "list", insert: "list", hint: "list active hooks" },
          { label: "trust", insert: "trust", hint: "trust this project's hooks" },
        ],
        "/model": [
          { label: "deepseek/deepseek-v4-flash", insert: "deepseek/deepseek-v4-flash", hint: "current" },
          { label: "deepseek/deepseek-v4-pro", insert: "deepseek/deepseek-v4-pro", hint: "" },
        ],
        "/effort": [
          { label: "auto", insert: "auto", hint: "use the model default" },
          { label: "high", insert: "high", hint: "deeper reasoning" },
          { label: "max", insert: "max", hint: "maximum reasoning" },
        ],
      };
      const items = (subs[cmd] ?? [])
        .filter((it) => it.label.toLowerCase().startsWith(cur.toLowerCase()))
        .map((it) => ({ label: it.label, insert: it.insert, hint: it.hint, descend: it.descend ?? false }));
      return { items, from };
    },
    async ListDir(rel: string) {
      // A tiny fake tree so the @ menu is navigable in browser dev.
      if (rel === "" || rel === "./") {
        return [
          { name: "internal", isDir: true },
          { name: "desktop", isDir: true },
          { name: "README.md", isDir: false },
          { name: "go.mod", isDir: false },
        ];
      }
      if (rel === "internal/") {
        return [
          { name: "control", isDir: true },
          { name: "boot", isDir: true },
          { name: "event.go", isDir: false },
        ];
      }
      return [{ name: "file.go", isDir: false }];
    },
    async SearchFileRefs(query: string) {
      const q = query.toLowerCase();
      return ["desktop/frontend/src/lib/bridge.ts", "frontend/wailsjs/runtime/runtime.js", "internal/control/refs.go"]
        .filter((path) => path.split("/").pop()?.toLowerCase().includes(q))
        .map((name) => ({ name, isDir: false }));
    },
    async ReadFile(rel: string) {
      const samples: Record<string, string> = {
        "README.md": "# Reasonix\n\nBrowser-dev workspace preview.\n\n- Chat in the center\n- Browse files on the right\n- Keep sessions on the left\n",
        "go.mod": "module reasonix\n\ngo 1.23\n",
        "desktop/file.go": "package desktop\n\nfunc main() {\n\tprintln(\"workspace preview\")\n}\n",
        "internal/event.go": "package internal\n\n// mock file used by the browser dev seam\n",
      };
      return {
        path: rel,
        body: samples[rel] ?? `// ${rel}\n\nMock file body from browser dev.`,
        size: samples[rel]?.length ?? 42,
        truncated: false,
        binary: false,
      };
    },
    async WorkspaceChanges() {
      return {
        gitAvailable: true,
        files: [
          {
            path: "desktop/frontend/src/components/WorkspacePanel.tsx",
            sources: ["session", "git"],
            gitStatus: "M",
            turns: [0, 2],
            latestPrompt: "Mock session edited the workspace panel.",
            latestTime: Date.now() - 60_000,
          },
          { path: "README.md", sources: ["git"], gitStatus: "??" },
          { path: "internal/control/controller.go", sources: ["session"], turns: [1], latestTime: Date.now() - 120_000 },
        ],
      };
    },
    async OpenWorkspacePath(rel: string) {
      console.info("mock OpenWorkspacePath", rel);
    },
    async RevealWorkspacePath(rel: string) {
      console.info("mock RevealWorkspacePath", rel);
    },
    async SavePastedImage(_dataUrl: string) {
      return ".reasonix/attachments/mock.png";
    },
    async SavePastedFile(name: string, _dataUrl: string) {
      return `.reasonix/attachments/mock-${name}`;
    },
    async AttachDropped(path: string) {
      const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
      return { kind: "attachment" as const, path: `.reasonix/attachments/mock-${name}` };
    },
    async AttachmentDataURL(_path: string) {
      return "data:image/png;base64,iVBORw0KGgo=";
    },
    async Models() {
      return [
        { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash", current: true },
        { ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro", current: false },
      ];
    },
    async SetModel() {},
    async Effort() {
      return { supported: true, current: mockEffort, default: "high", levels: ["auto", "high", "max"] };
    },
    async SetEffort(level: string) {
      mockEffort = level || "auto";
    },
    async Memory() {
      return {
        available: true,
        storeDir: "~/.config/reasonix/projects/-mock/memory",
        docs: [
          {
            path: "REASONIX.md",
            scope: "project",
            body: "# Reasonix project memory\n\nMock doc shown in the browser dev seam.\n\n## Notes\n\n- prefers concise replies",
          },
          {
            path: "~/.config/reasonix/REASONIX.md",
            scope: "user",
            body: "# User memory\n\nAlways respond in 中文.",
          },
        ],
        facts: [
          {
            name: "prefers-tabs",
            description: "User prefers tabs",
            type: "user",
            body: "Indent with tabs.",
          },
        ],
        scopes: [
          { scope: "user", path: "~/.config/reasonix/REASONIX.md" },
          { scope: "project", path: "REASONIX.md" },
          { scope: "local", path: "REASONIX.local.md" },
        ],
      };
    },
    async Remember(scope: string, note: string) {
      emit({ kind: "notice", level: "info", text: `remembered → ${scope}` });
      return `${scope} REASONIX.md (mock): ${note}`;
    },
    async Forget(name: string) {
      emit({ kind: "notice", level: "info", text: `forgot → ${name}` });
    },
    async SaveDoc(path: string, _body: string) {
      emit({ kind: "notice", level: "info", text: `saved → ${path}` });
      return path;
    },
    async Settings() {
      return JSON.parse(JSON.stringify(settings)) as SettingsView;
    },
    async SetDefaultModel(ref: string) {
      settings.defaultModel = ref;
    },
    async SetPlannerModel(ref: string) {
      settings.plannerModel = ref;
    },
    async SaveProvider(p: ProviderView) {
      const i = settings.providers.findIndex((x) => x.name === p.name);
      if (i >= 0) settings.providers[i] = p;
      else settings.providers.push(p);
    },
    async DeleteProvider(name: string) {
      settings.providers = settings.providers.filter((p) => p.name !== name);
    },
    async SetProviderKey(apiKeyEnv: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = true;
      });
    },
    async SetPermissionMode(mode: string) {
      settings.permissions.mode = mode;
    },
    async AddPermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      if (settings.permissions[k] && !settings.permissions[k].includes(rule)) settings.permissions[k].push(rule);
    },
    async RemovePermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      settings.permissions[k] = settings.permissions[k].filter((r) => r !== rule);
    },
	    async SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[]) {
	      settings.sandbox = { bash, network, workspaceRoot, allowWrite };
	    },
	    async SetNetwork(n: NetworkView) {
	      settings.network = n;
	    },
    async SetAgentParams(temperature: number, maxSteps: number, systemPrompt: string) {
      settings.agent = { temperature, maxSteps, systemPrompt };
    },
    async SetBypass(on: boolean) {
      settings.bypass = on;
    },
    async Version() {
      return "v1.0.0 (browser dev)";
    },
    async CheckUpdate() {
      // Dev mock advertises an update so the banner and apply flow are exercisable
      // in the browser without a real release behind it.
      return {
        available: true,
        current: "v1.0.0",
        latest: "v1.1.0",
        notes: "- Mock release notes\n- The **Update now** button streams a fake download here.",
        canSelfUpdate: true,
        downloadUrl: "https://github.com/esengine/reasonix/releases/latest",
        assetSize: 12_345_678,
      };
    },
    async ApplyUpdate() {
      const total = 12_345_678;
      for (let r = 0; r <= total; r += 1_800_000) {
        emitUpdater({ phase: "downloading", received: Math.min(r, total), total });
        await delay(120);
      }
      emitUpdater({ phase: "verifying", received: total, total });
      await delay(500);
      emitUpdater({ phase: "applying", received: total, total });
      await delay(500);
      emitUpdater({ phase: "done", received: total, total });
      // The real shell relaunches here; the mock just stops.
    },
    async OpenDownloadPage() {
      if (typeof window !== "undefined") {
        window.open("https://github.com/esengine/reasonix/releases/latest", "_blank", "noopener");
      }
    },
    // Dev seam: drives the overlay flow in the browser until ConnectKey sets the
    // key. Matches ConnectKey on apiKeyEnv so the two stay in sync.
    async NeedsOnboarding() {
      return !settings.providers.find((p) => p.apiKeyEnv === "DEEPSEEK_API_KEY")?.keySet;
    },
    async ConnectKey(apiKey: string) {
      if (!apiKey.trim()) throw new Error("key is required");
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === "DEEPSEEK_API_KEY") p.keySet = true;
      });
      await delay(300);
    },
    // Tab management mocks.
    async ListTabs() {
      return [
        {
          id: "tab_mock_1",
          scope: "global",
          workspaceRoot: "",
          workspaceName: "Global",
          topicId: "",
          topicTitle: "Global",
          label: "deepseek-v4-flash",
          ready: true,
          running: false,
          active: true,
          cwd: "~/projects/reasonix",
        },
        {
          id: "tab_mock_2",
          scope: "project",
          workspaceRoot: "~/projects/blade",
          workspaceName: "blade",
          topicId: "topic_mock_1",
          topicTitle: "Fix login bug",
          label: "deepseek-v4-pro",
          ready: true,
          running: false,
          active: false,
          cwd: "~/projects/blade",
        },
      ];
    },
    async OpenProjectTab(workspaceRoot: string, _topicID: string) {
      return {
        id: "tab_" + Date.now(),
        scope: "project",
        workspaceRoot,
        workspaceName: workspaceRoot.split("/").filter(Boolean).pop() ?? workspaceRoot,
        topicId: _topicID,
        topicTitle: "New topic",
        label: "deepseek-v4-flash",
        ready: true,
        running: false,
        active: true,
        cwd: workspaceRoot,
      };
    },
    async OpenGlobalTab(_topicID: string) {
      return {
        id: "tab_" + Date.now(),
        scope: "global",
        workspaceRoot: "",
        workspaceName: "Global",
        topicId: _topicID,
        topicTitle: "Global",
        label: "deepseek-v4-flash",
        ready: true,
        running: false,
        active: true,
        cwd: "",
      };
    },
    async SetActiveTab(_tabID: string) {},
    async CloseTab(_tabID: string) {},
    async ListProjectTree() {
      return [
        {
          key: "global_folder",
          kind: "global_folder" as const,
          label: "Global",
          children: [
            { key: "global_topic_1", kind: "global_topic" as const, label: "● General", topicId: "topic_global_1" },
          ],
        },
        {
          key: "project_~/projects/reasonix",
          kind: "project" as const,
          label: "reasonix",
          root: "~/projects/reasonix",
          children: [
            { key: "topic_t1", kind: "topic" as const, label: "● Fix auth flow", root: "~/projects/reasonix", topicId: "topic_t1" },
            { key: "topic_t2", kind: "topic" as const, label: "Refactor config", root: "~/projects/reasonix", topicId: "topic_t2" },
          ],
        },
        {
          key: "project_~/projects/blade",
          kind: "project" as const,
          label: "blade",
          root: "~/projects/blade",
          children: [
            { key: "topic_t3", kind: "topic" as const, label: "● Fix login bug", root: "~/projects/blade", topicId: "topic_t3" },
          ],
        },
      ];
    },
    async CreateTopic(_scope: string, _workspaceRoot: string, title: string) {
      return { id: "topic_" + Date.now(), title, createdAt: Date.now() };
    },
    async RenameTopic(_topicID: string, _title: string) {},
    async DeleteTopic(_topicID: string) {},
    async ContextPanel(_tabID: string) {
      return {
        usedTokens: 1280,
        windowTokens: 1000000,
        promptTokens: 1200,
        completionTokens: 80,
        reasoningTokens: 0,
        cacheHitTokens: 1024,
        cacheMissTokens: 256,
        sessionCostUsd: 0.05,
        readFiles: [
          { path: "src/app.ts", turn: 1, time: Date.now() - 60000 },
          { path: "src/lib/bridge.ts", turn: 1, time: Date.now() - 55000, offset: 100, limit: 50 },
        ],
        changedFiles: [
          { path: "src/lib/bridge.ts", sources: ["session"], turns: [1], latestPrompt: "add tab management", latestTime: Date.now() - 60000 },
        ],
      };
    },
  };
}
