// bridge is the single seam between the React app and the Go kernel. In the Wails
// shell it calls the bound App methods (window.go.main.App.*) and subscribes to
// the runtime event stream (window.runtime.EventsOn). In a plain browser (`pnpm
// dev` outside the shell) those globals are absent, so it falls back to a mock
// that streams a canned turn through the same contract — letting the whole UI be
// developed and laid out without rebuilding the Go side.

import type {
  BalanceInfo,
  CheckpointMeta,
  CommandInfo,
  ContextInfo,
  DirEntry,
  HistoryMessage,
  JobView,
  MemoryView,
  Meta,
  ModelInfo,
  ProviderView,
  QuestionAnswer,
  SessionMeta,
  SettingsView,
  SlashArgsResult,
  UpdateInfo,
  UpdateProgress,
  WireEvent,
} from "./types";

// AppBindings mirrors desktop/app.go's exported method set. Keep in sync by hand
// (or regenerate with `wails generate module` and import wailsjs instead).
export interface AppBindings {
  Submit(input: string): Promise<void>;
  Cancel(): Promise<void>;
  Approve(id: string, allow: boolean, session: boolean): Promise<void>;
  AnswerQuestion(id: string, answers: QuestionAnswer[]): Promise<void>;
  SetPlanMode(on: boolean): Promise<void>;
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
  // delete one, or give one a custom display name ("" clears it).
  ListSessions(): Promise<SessionMeta[]>;
  ResumeSession(path: string): Promise<HistoryMessage[]>;
  DeleteSession(path: string): Promise<void>;
  RenameSession(path: string, title: string): Promise<void>;
  // Workspace: open a folder chooser and switch to that project (fresh session);
  // returns the chosen path, or "" if cancelled.
  PickWorkspace(): Promise<string>;
  ContextUsage(): Promise<ContextInfo>;
  // Balance queries the active provider's wallet balance (a network call);
  // returns an unavailable readout when no balance_url is configured or it fails.
  Balance(): Promise<BalanceInfo>;
  // Jobs lists the running background jobs (bash/task started in the background)
  // for the status-bar indicator.
  Jobs(): Promise<JobView[]>;
  Meta(): Promise<Meta>;
  Commands(): Promise<CommandInfo[]>;
  SlashArgs(input: string): Promise<SlashArgsResult>;
  ListDir(rel: string): Promise<DirEntry[]>;
  SavePastedImage(dataUrl: string): Promise<string>;
  AttachmentDataURL(path: string): Promise<string>;
  Models(): Promise<ModelInfo[]>;
  SetModel(name: string): Promise<void>;
  // Memory panel: read the loaded REASONIX.md hierarchy + saved auto-memories,
  // quick-add a note to a scope's REASONIX.md (≡ "#<note>"), and overwrite a doc
  // from the in-place editor.
  Memory(): Promise<MemoryView>;
  Remember(scope: string, note: string): Promise<string>;
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
  SetAgentParams(temperature: number, maxSteps: number, systemPrompt: string): Promise<void>;
  SetLanguage(lang: string): Promise<void>;
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
}

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
  BrowserOpenURL(url: string): void;
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
  let cwd = "~/projects/reasonix"; // mutable so PickWorkspace is visible in dev
  const day = 86_400_000;
  const t0 = Date.now();
  // Mutable so delete/rename are observable in browser dev.
  const sessions: SessionMeta[] = [
    { path: "/mock/sessions/a.jsonl", preview: "fix the login bug in auth.go", turns: 12, modTime: t0 - 3_600_000, current: true },
    { path: "/mock/sessions/b.jsonl", preview: "refactor the payment module", turns: 5, modTime: t0 - 6 * 3_600_000, current: false },
    { path: "/mock/sessions/c.jsonl", preview: "write the README and badges", turns: 8, modTime: t0 - day - 3_600_000, current: false },
    { path: "/mock/sessions/d.jsonl", preview: "explain the plugin host design", turns: 3, modTime: t0 - 4 * day, current: false },
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
    agent: { temperature: 0.2, maxSteps: 0, systemPrompt: "You are Reasonix, a coding agent." },
    language: "",
    configPath: "~/projects/reasonix/reasonix.toml",
    providerKinds: ["openai"],
    bypass: false,
  };
  return {
    async Submit(input) {
      cancelled = false;
      emit({ kind: "turn_started" });
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
    async Cancel() {
      cancelled = true;
      emit({ kind: "turn_done" });
    },
    async Approve() {},
    async AnswerQuestion() {},
    async SetPlanMode() {},
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
      return [
        { role: "user", content: `(mock) resumed ${path}` },
        { role: "assistant", content: "This is a mock resumed transcript — the real one comes from the kernel." },
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
    async PickWorkspace() {
      // Browser dev has no native dialog; simulate picking a folder and re-root so
      // the topbar folder chip visibly changes.
      cwd = cwd.endsWith("another-project") ? "~/projects/reasonix" : "~/projects/another-project";
      return cwd;
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
        { name: "skill", description: "List skills", kind: "builtin" as const },
        { name: "explore", description: "Investigate the codebase in an isolated subagent", kind: "skill" as const },
        { name: "review", description: "Review the staged diff", hint: "[focus]", kind: "custom" as const },
      ];
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
    async SavePastedImage(_dataUrl: string) {
      return ".reasonix/attachments/mock.png";
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
    async SetAgentParams(temperature: number, maxSteps: number, systemPrompt: string) {
      settings.agent = { temperature, maxSteps, systemPrompt };
    },
    async SetLanguage(lang: string) {
      settings.language = lang;
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
  };
}
