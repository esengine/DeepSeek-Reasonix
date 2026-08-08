import type { ReasonixHost, HostCapabilities } from "./types";
import { POC_CAPABILITIES } from "./capabilities";
import { SERVE_ROUTES, tabPath } from "./routes";
import { mapServeEventToWire, parseSseChunk } from "./mapEvent";

/**
 * HTTP JSON + SSE host talking to reasonix serve.
 * Mirrors electron-poc/lib/httpSseHost.mjs (keep behavior aligned).
 */
export class HttpSseHost implements ReasonixHost {
  private baseUrl: string;
  private token: string;
  private fetchImpl: typeof fetch;
  private capabilities: HostCapabilities;
  private abort: AbortController | null = null;
  private handlers = new Set<(e: Record<string, unknown>) => void>();
  private sseTask: Promise<void> | null = null;

  constructor(opts: {
    baseUrl: string;
    token: string;
    fetchImpl?: typeof fetch;
    capabilities?: HostCapabilities;
  }) {
    if (!opts.baseUrl) throw new Error("baseUrl required");
    if (!opts.token) throw new Error("token required");
    this.baseUrl = opts.baseUrl.replace(/\/$/, "");
    this.token = opts.token;
    this.fetchImpl = opts.fetchImpl ?? fetch.bind(globalThis);
    this.capabilities = opts.capabilities ?? POC_CAPABILITIES;
  }

  getCapabilities(): HostCapabilities {
    return this.capabilities;
  }

  private async request(path: string, init: RequestInit & { json?: unknown } = {}): Promise<Response> {
    const url = new URL(path, this.baseUrl + "/");
    // Token query is a fallback for non-browser clients. Electron shell proxies
    // should auth via cookie they inject; attaching ?token= can trigger serve's
    // 302 strip-token redirect which becomes 401 when Cookie is a forbidden
    // header in Chromium and a stale cookie is present on the shell origin.
    if (!url.searchParams.has("token")) url.searchParams.set("token", this.token);
    const headers = new Headers(init.headers ?? {});
    // Best-effort: Node hosts honor this; Chromium silently drops Cookie.
    headers.set("Cookie", `reasonix_token=${this.token}`);
    let body = init.body;
    if (init.json !== undefined) {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(init.json);
    }
    const method = (init.method ?? "GET").toUpperCase();
    if (method === "POST" && !headers.get("Content-Type")?.includes("application/json")) {
      headers.set("Content-Type", "application/json");
      if (body === undefined) body = "{}";
    }
    // Do not follow auth redirects (302 strip ?token= → unauthenticated retry).
    return this.fetchImpl(url.toString(), {
      ...init,
      method,
      headers,
      body,
      redirect: init.redirect ?? "manual",
    });
  }

  private async jsonOrEmpty(res: Response): Promise<unknown> {
    if (res.status === 204 || res.status === 202) return { ok: true, status: res.status };
    const text = await res.text();
    if (!res.ok) {
      const err = new Error(text || res.statusText || `HTTP ${res.status}`) as Error & {
        status?: number;
        body?: string;
      };
      err.status = res.status;
      err.body = text;
      throw err;
    }
    if (!text) return { ok: true, status: res.status };
    try {
      return JSON.parse(text);
    } catch {
      return { ok: true, status: res.status, raw: text };
    }
  }

  status() {
    return this.request(SERVE_ROUTES.status.path).then((r) => this.jsonOrEmpty(r));
  }
  history() {
    return this.request(SERVE_ROUTES.history.path).then((r) => this.jsonOrEmpty(r));
  }
  context() {
    return this.request(SERVE_ROUTES.context.path).then((r) => this.jsonOrEmpty(r));
  }
  sessions() {
    return this.request(SERVE_ROUTES.sessions.path).then((r) => this.jsonOrEmpty(r));
  }
  skills() {
    return this.request(SERVE_ROUTES.skills.path).then((r) => this.jsonOrEmpty(r));
  }
  todos() {
    return this.request(SERVE_ROUTES.todos.path).then((r) => this.jsonOrEmpty(r));
  }
  checkpoints() {
    return this.request(SERVE_ROUTES.checkpoints.path).then((r) => this.jsonOrEmpty(r));
  }
  models() {
    return this.request(SERVE_ROUTES.models.path).then((r) => this.jsonOrEmpty(r));
  }
  providerSetup() {
    return this.request(SERVE_ROUTES.providerSetup.path).then((r) => this.jsonOrEmpty(r));
  }

  submit(input: string, format?: string) {
    const json: Record<string, string> = { input };
    if (format) json.format = format;
    return this.request(SERVE_ROUTES.submit.path, { method: "POST", json }).then((r) => this.jsonOrEmpty(r));
  }
  cancel() {
    return this.request(SERVE_ROUTES.cancel.path, { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  approve(id: string, allow: boolean, session = false, persist = false) {
    return this.request(SERVE_ROUTES.approve.path, {
      method: "POST",
      json: { id, allow, session, persist },
    }).then((r) => this.jsonOrEmpty(r));
  }
  answer(id: string, answers: unknown) {
    return this.request(SERVE_ROUTES.answer.path, {
      method: "POST",
      json: { id, answers },
    }).then((r) => this.jsonOrEmpty(r));
  }
  setPlanMode(on: boolean) {
    return this.request(SERVE_ROUTES.plan.path, { method: "POST", json: { on: !!on } }).then((r) =>
      this.jsonOrEmpty(r),
    );
  }
  setToolApprovalMode(mode: string) {
    return this.request(SERVE_ROUTES.toolApprovalMode.path, {
      method: "POST",
      json: { mode },
    }).then((r) => this.jsonOrEmpty(r));
  }
  setAutoApproveTools(on: boolean) {
    return this.request(SERVE_ROUTES.autoApproveTools.path, {
      method: "POST",
      json: { on: !!on },
    }).then((r) => this.jsonOrEmpty(r));
  }
  compact() {
    return this.request(SERVE_ROUTES.compact.path, { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  newSession() {
    return this.request(SERVE_ROUTES.newSession.path, { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  rewind(turn: number, scope: string) {
    return this.request(SERVE_ROUTES.rewind.path, {
      method: "POST",
      json: { turn, scope },
    }).then((r) => this.jsonOrEmpty(r));
  }
  fork(turn: number) {
    return this.request(SERVE_ROUTES.fork.path, { method: "POST", json: { turn } }).then((r) => this.jsonOrEmpty(r));
  }
  summarize(turn: number) {
    return this.request(SERVE_ROUTES.summarize.path, {
      method: "POST",
      json: { turn },
    }).then((r) => this.jsonOrEmpty(r));
  }
  setGoal(goal: string) {
    return this.request(SERVE_ROUTES.goal.path, {
      method: "POST",
      json: { goal: goal ?? "" },
    }).then((r) => this.jsonOrEmpty(r));
  }
  clearGoal() {
    return this.setGoal("");
  }
  resume(path: string) {
    return this.request(SERVE_ROUTES.resume.path, {
      method: "POST",
      json: { path },
    }).then((r) => this.jsonOrEmpty(r));
  }
  deleteSession(path: string) {
    return this.request(SERVE_ROUTES.deleteSession.path, {
      method: "POST",
      json: { path },
    }).then((r) => this.jsonOrEmpty(r));
  }
  reloadExtensions() {
    return this.request(SERVE_ROUTES.reloadExtensions.path, {
      method: "POST",
      json: {},
    }).then((r) => this.jsonOrEmpty(r));
  }

  // ── Multi-tab (--multi-tab) ────────────────────────────────

  listTabs() {
    return this.request(SERVE_ROUTES.tabs.path).then((r) => this.jsonOrEmpty(r));
  }
  createTab(body: {
    workspaceRoot?: string;
    scope?: string;
    topicId?: string;
    topicTitle?: string;
    sessionPath?: string;
    label?: string;
  }) {
    return this.request(SERVE_ROUTES.tabsCreate.path, { method: "POST", json: body ?? {} }).then((r) =>
      this.jsonOrEmpty(r),
    );
  }
  openProject(workspaceRoot: string, topicId?: string, topicTitle?: string) {
    return this.request(SERVE_ROUTES.tabsOpenProject.path, {
      method: "POST",
      json: { workspaceRoot, topicId, topicTitle },
    }).then((r) => this.jsonOrEmpty(r));
  }

  // ── Desktop sidebar / settings ─────────────────────────────
  projectTree() {
    return this.request(SERVE_ROUTES.desktopProjectTree.path).then((r) => this.jsonOrEmpty(r));
  }
  createTopic(scope: string, workspaceRoot: string, title: string) {
    return this.request(SERVE_ROUTES.desktopCreateTopic.path, {
      method: "POST",
      json: { scope, workspaceRoot, title },
    }).then((r) => this.jsonOrEmpty(r));
  }
  renameTopic(workspaceRoot: string, topicId: string, title: string) {
    return this.request(SERVE_ROUTES.desktopRenameTopic.path, {
      method: "POST",
      json: { workspaceRoot, topicId, title },
    }).then((r) => this.jsonOrEmpty(r));
  }
  deleteTopic(workspaceRoot: string, topicId: string) {
    return this.request(SERVE_ROUTES.desktopDeleteTopic.path, {
      method: "POST",
      json: { workspaceRoot, topicId },
    }).then((r) => this.jsonOrEmpty(r));
  }
  trashTopic(workspaceRoot: string, topicId: string) {
    return this.request(SERVE_ROUTES.desktopTrashTopic.path, {
      method: "POST",
      json: { workspaceRoot, topicId },
    }).then((r) => this.jsonOrEmpty(r));
  }
  removeProject(workspaceRoot: string) {
    return this.request(SERVE_ROUTES.desktopRemoveProject.path, {
      method: "POST",
      json: { workspaceRoot },
    }).then((r) => this.jsonOrEmpty(r));
  }
  renameProject(workspaceRoot: string, title: string) {
    return this.request(SERVE_ROUTES.desktopRenameProject.path, {
      method: "POST",
      json: { workspaceRoot, title },
    }).then((r) => this.jsonOrEmpty(r));
  }
  reorderProjects(workspaceRoots: string[]) {
    return this.request(SERVE_ROUTES.desktopReorderProjects.path, {
      method: "POST",
      json: { workspaceRoots },
    }).then((r) => this.jsonOrEmpty(r));
  }
  desktopStartupSettings() {
    return this.request(SERVE_ROUTES.desktopStartupSettings.path).then((r) => this.jsonOrEmpty(r));
  }
  desktopSettings() {
    return this.request(SERVE_ROUTES.desktopSettings.path).then((r) => this.jsonOrEmpty(r));
  }
  activateTab(id: string) {
    return this.request(tabPath(id, "activate"), { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  closeTab(id: string) {
    return this.request(tabPath(id, "close"), { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  submitTab(id: string, input: string, format?: string) {
    const json: Record<string, string> = { input };
    if (format) json.format = format;
    return this.request(tabPath(id, "submit"), { method: "POST", json }).then((r) => this.jsonOrEmpty(r));
  }
  cancelTab(id: string) {
    return this.request(tabPath(id, "cancel"), { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  approveTab(tabId: string, id: string, allow: boolean, session = false, persist = false) {
    return this.request(tabPath(tabId, "approve"), {
      method: "POST",
      json: { id, allow, session, persist },
    }).then((r) => this.jsonOrEmpty(r));
  }
  answerTab(tabId: string, id: string, answers: unknown) {
    return this.request(tabPath(tabId, "answer"), {
      method: "POST",
      json: { id, answers },
    }).then((r) => this.jsonOrEmpty(r));
  }
  setPlanModeTab(id: string, on: boolean) {
    return this.request(tabPath(id, "plan"), { method: "POST", json: { on: !!on } }).then((r) =>
      this.jsonOrEmpty(r),
    );
  }
  compactTab(id: string) {
    return this.request(tabPath(id, "compact"), { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  newSessionTab(id: string) {
    return this.request(tabPath(id, "new"), { method: "POST", json: {} }).then((r) => this.jsonOrEmpty(r));
  }
  setGoalTab(id: string, goal: string) {
    return this.request(tabPath(id, "goal"), { method: "POST", json: { goal: goal ?? "" } }).then((r) =>
      this.jsonOrEmpty(r),
    );
  }
  setToolApprovalModeTab(id: string, mode: string) {
    return this.request(tabPath(id, "tool-approval-mode"), {
      method: "POST",
      json: { mode },
    }).then((r) => this.jsonOrEmpty(r));
  }
  historyTab(id: string) {
    return this.request(tabPath(id, "history")).then((r) => this.jsonOrEmpty(r));
  }
  contextTab(id: string) {
    return this.request(tabPath(id, "context")).then((r) => this.jsonOrEmpty(r));
  }
  statusTab(id: string) {
    return this.request(tabPath(id, "status")).then((r) => this.jsonOrEmpty(r));
  }

  /** Prefer tab-scoped submit when multi-tab is available; fall back to legacy. */
  async submitPreferTab(tabId: string | undefined, input: string, format?: string) {
    if (tabId) {
      try {
        return await this.submitTab(tabId, input, format);
      } catch (e) {
        const err = e as { status?: number };
        if (err.status !== 404) throw e;
      }
    }
    return this.submit(input, format);
  }

  onEvent(handler: (e: Record<string, unknown>) => void): () => void {
    this.handlers.add(handler);
    if (!this.sseTask) this.sseTask = this.runSseLoop();
    return () => {
      this.handlers.delete(handler);
      if (this.handlers.size === 0) this.dispose();
    };
  }

  private async runSseLoop(): Promise<void> {
    this.abort = new AbortController();
    const state: { pending?: string } = { pending: "" };
    let backoff = 250;
    while (this.handlers.size > 0 && !this.abort.signal.aborted) {
      try {
        const url = new URL(SERVE_ROUTES.events.path, this.baseUrl + "/");
        url.searchParams.set("token", this.token);
        const res = await this.fetchImpl(url.toString(), {
          headers: {
            Accept: "text/event-stream",
            Cookie: `reasonix_token=${this.token}`,
          },
          signal: this.abort.signal,
          redirect: "manual",
        });
        if (!res.ok || !res.body) throw new Error(`SSE HTTP ${res.status}`);
        backoff = 250;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        while (this.handlers.size > 0) {
          const { done, value } = await reader.read();
          if (done) break;
          const text = decoder.decode(value, { stream: true });
          for (const data of parseSseChunk(text, state)) {
            const wire = mapServeEventToWire(data);
            if (!wire) continue;
            for (const h of this.handlers) {
              try {
                h(wire);
              } catch {
                /* ignore */
              }
            }
          }
        }
      } catch {
        if (this.abort?.signal.aborted) break;
        await new Promise((r) => setTimeout(r, backoff));
        backoff = Math.min(backoff * 2, 8_000);
      }
    }
    this.sseTask = null;
  }

  dispose(): void {
    this.handlers.clear();
    try {
      this.abort?.abort();
    } catch {
      /* ignore */
    }
    this.abort = null;
    this.sseTask = null;
  }
}

export function createHttpSseHost(opts: {
  baseUrl: string;
  token: string;
  fetchImpl?: typeof fetch;
}): HttpSseHost {
  return new HttpSseHost(opts);
}
