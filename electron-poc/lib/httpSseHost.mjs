import { SERVE_ROUTES, tabPath } from "./routes.mjs";
import { POC_CAPABILITIES } from "./capabilities.mjs";
import { mapServeEventToWire, parseSseChunk } from "./mapEvent.mjs";

/**
 * Pluggable ReasonixHost over HTTP JSON + SSE (reasonix serve).
 * Auth: cookie reasonix_token (serve token mode). Also supports ?token= on first request.
 */
export class HttpSseHost {
  /**
   * @param {{
   *   baseUrl: string,
   *   token: string,
   *   fetchImpl?: typeof fetch,
   *   capabilities?: typeof POC_CAPABILITIES,
   * }} opts
   */
  constructor(opts) {
    if (!opts?.baseUrl) throw new Error("baseUrl required");
    if (!opts?.token) throw new Error("token required");
    this.baseUrl = opts.baseUrl.replace(/\/$/, "");
    this.token = opts.token;
    this.fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis);
    this.capabilities = opts.capabilities ?? POC_CAPABILITIES;
    this._abort = null;
    this._handlers = new Set();
    this._sseTask = null;
  }

  /** @returns {typeof POC_CAPABILITIES} */
  getCapabilities() {
    return this.capabilities;
  }

  /**
   * @param {string} path
   * @param {RequestInit & { json?: unknown }} [init]
   */
  async request(path, init = {}) {
    const url = new URL(path, this.baseUrl + "/");
    // Prefer cookie auth for browser-like clients; also attach query for APIs without cookie jar
    if (!url.searchParams.has("token")) {
      url.searchParams.set("token", this.token);
    }
    const headers = new Headers(init.headers ?? {});
    headers.set("Cookie", `reasonix_token=${this.token}`);
    let body = init.body;
    if (init.json !== undefined) {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(init.json);
    }
    const method = (init.method ?? "GET").toUpperCase();
    if (method === "POST" && !headers.get("Content-Type")?.includes("application/json")) {
      // Serve csrfGuard requires application/json on POST
      headers.set("Content-Type", "application/json");
      if (body === undefined) body = "{}";
    }
    const res = await this.fetchImpl(url.toString(), {
      ...init,
      method,
      headers,
      body,
      // Avoid following serve auth 302 (token strip) into an unauthenticated hop.
      redirect: init.redirect ?? "manual",
    });
    return res;
  }

  async _jsonOrEmpty(res) {
    if (res.status === 204 || res.status === 202) {
      return { ok: true, status: res.status };
    }
    const text = await res.text();
    if (!res.ok) {
      const err = new Error(text || res.statusText || `HTTP ${res.status}`);
      // @ts-ignore
      err.status = res.status;
      // @ts-ignore
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

  // ── P0 queries ──────────────────────────────────────────

  async status() {
    const res = await this.request(SERVE_ROUTES.status.path);
    return this._jsonOrEmpty(res);
  }

  async history() {
    const res = await this.request(SERVE_ROUTES.history.path);
    return this._jsonOrEmpty(res);
  }

  async context() {
    const res = await this.request(SERVE_ROUTES.context.path);
    return this._jsonOrEmpty(res);
  }

  async sessions() {
    const res = await this.request(SERVE_ROUTES.sessions.path);
    return this._jsonOrEmpty(res);
  }

  async skills() {
    const res = await this.request(SERVE_ROUTES.skills.path);
    return this._jsonOrEmpty(res);
  }

  async todos() {
    const res = await this.request(SERVE_ROUTES.todos.path);
    return this._jsonOrEmpty(res);
  }

  async checkpoints() {
    const res = await this.request(SERVE_ROUTES.checkpoints.path);
    return this._jsonOrEmpty(res);
  }

  async branches() {
    const res = await this.request(SERVE_ROUTES.branches.path);
    return this._jsonOrEmpty(res);
  }

  async models() {
    const res = await this.request(SERVE_ROUTES.models.path);
    return this._jsonOrEmpty(res);
  }

  async providerSetup() {
    const res = await this.request(SERVE_ROUTES.providerSetup.path);
    return this._jsonOrEmpty(res);
  }

  // ── P0 commands ─────────────────────────────────────────

  async submit(input, format) {
    const json = { input };
    if (format) json.format = format;
    const res = await this.request(SERVE_ROUTES.submit.path, { method: "POST", json });
    return this._jsonOrEmpty(res);
  }

  async cancel() {
    const res = await this.request(SERVE_ROUTES.cancel.path, { method: "POST", json: {} });
    return this._jsonOrEmpty(res);
  }

  async approve(id, allow, session = false, persist = false) {
    const res = await this.request(SERVE_ROUTES.approve.path, {
      method: "POST",
      json: { id, allow, session, persist },
    });
    return this._jsonOrEmpty(res);
  }

  async answer(id, answers) {
    const res = await this.request(SERVE_ROUTES.answer.path, {
      method: "POST",
      json: { id, answers },
    });
    return this._jsonOrEmpty(res);
  }

  async setPlanMode(on) {
    const res = await this.request(SERVE_ROUTES.plan.path, { method: "POST", json: { on: !!on } });
    return this._jsonOrEmpty(res);
  }

  async setToolApprovalMode(mode) {
    const res = await this.request(SERVE_ROUTES.toolApprovalMode.path, {
      method: "POST",
      json: { mode },
    });
    return this._jsonOrEmpty(res);
  }

  async setAutoApproveTools(on) {
    const res = await this.request(SERVE_ROUTES.autoApproveTools.path, {
      method: "POST",
      json: { on: !!on },
    });
    return this._jsonOrEmpty(res);
  }

  async compact() {
    const res = await this.request(SERVE_ROUTES.compact.path, { method: "POST", json: {} });
    return this._jsonOrEmpty(res);
  }

  async newSession() {
    const res = await this.request(SERVE_ROUTES.newSession.path, { method: "POST", json: {} });
    return this._jsonOrEmpty(res);
  }

  async rewind(turn, scope) {
    const res = await this.request(SERVE_ROUTES.rewind.path, {
      method: "POST",
      json: { turn, scope },
    });
    return this._jsonOrEmpty(res);
  }

  async fork(turn) {
    const res = await this.request(SERVE_ROUTES.fork.path, { method: "POST", json: { turn } });
    return this._jsonOrEmpty(res);
  }

  async summarize(turn) {
    const res = await this.request(SERVE_ROUTES.summarize.path, {
      method: "POST",
      json: { turn },
    });
    return this._jsonOrEmpty(res);
  }

  async setGoal(goal) {
    const res = await this.request(SERVE_ROUTES.goal.path, {
      method: "POST",
      json: { goal: goal ?? "" },
    });
    return this._jsonOrEmpty(res);
  }

  async clearGoal() {
    return this.setGoal("");
  }

  async resume(path) {
    const res = await this.request(SERVE_ROUTES.resume.path, {
      method: "POST",
      json: { path },
    });
    return this._jsonOrEmpty(res);
  }

  async deleteSession(path) {
    const res = await this.request(SERVE_ROUTES.deleteSession.path, {
      method: "POST",
      json: { path },
    });
    return this._jsonOrEmpty(res);
  }

  // Multi-tab
  async listTabs() {
    return this._jsonOrEmpty(await this.request(SERVE_ROUTES.tabs.path));
  }
  async createTab(body = {}) {
    return this._jsonOrEmpty(await this.request(SERVE_ROUTES.tabsCreate.path, { method: "POST", json: body }));
  }
  async openProject(workspaceRoot, topicId, topicTitle) {
    return this._jsonOrEmpty(
      await this.request(SERVE_ROUTES.tabsOpenProject.path, {
        method: "POST",
        json: { workspaceRoot, topicId, topicTitle },
      }),
    );
  }
  async activateTab(id) {
    return this._jsonOrEmpty(await this.request(tabPath(id, "activate"), { method: "POST", json: {} }));
  }
  async closeTab(id) {
    return this._jsonOrEmpty(await this.request(tabPath(id, "close"), { method: "POST", json: {} }));
  }
  async submitTab(id, input, format) {
    const json = { input };
    if (format) json.format = format;
    return this._jsonOrEmpty(await this.request(tabPath(id, "submit"), { method: "POST", json }));
  }
  async cancelTab(id) {
    return this._jsonOrEmpty(await this.request(tabPath(id, "cancel"), { method: "POST", json: {} }));
  }
  async historyTab(id) {
    return this._jsonOrEmpty(await this.request(tabPath(id, "history")));
  }
  async statusTab(id) {
    return this._jsonOrEmpty(await this.request(tabPath(id, "status")));
  }

  async forget(path) {
    const res = await this.request(SERVE_ROUTES.forget.path, {
      method: "POST",
      json: { path },
    });
    return this._jsonOrEmpty(res);
  }

  async reloadExtensions() {
    const res = await this.request(SERVE_ROUTES.reloadExtensions.path, {
      method: "POST",
      json: {},
    });
    return this._jsonOrEmpty(res);
  }

  async saveProviderSetup(body) {
    const res = await this.request(SERVE_ROUTES.providerSetupSave.path, {
      method: "POST",
      json: body,
    });
    return this._jsonOrEmpty(res);
  }

  // ── Events ──────────────────────────────────────────────

  /**
   * @param {(e: Record<string, unknown>) => void} handler
   * @returns {() => void} unsubscribe
   */
  onEvent(handler) {
    this._handlers.add(handler);
    if (!this._sseTask) {
      this._sseTask = this._runSseLoop();
    }
    return () => {
      this._handlers.delete(handler);
      if (this._handlers.size === 0) {
        this.dispose();
      }
    };
  }

  async _runSseLoop() {
    this._abort = new AbortController();
    const state = { pending: "" };
    let backoff = 250;
    while (this._handlers.size > 0 && !this._abort.signal.aborted) {
      try {
        const url = new URL(SERVE_ROUTES.events.path, this.baseUrl + "/");
        url.searchParams.set("token", this.token);
        const res = await this.fetchImpl(url.toString(), {
          headers: {
            Accept: "text/event-stream",
            Cookie: `reasonix_token=${this.token}`,
          },
          signal: this._abort.signal,
        });
        if (!res.ok || !res.body) {
          throw new Error(`SSE HTTP ${res.status}`);
        }
        backoff = 250;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        while (this._handlers.size > 0) {
          const { done, value } = await reader.read();
          if (done) break;
          const text = decoder.decode(value, { stream: true });
          for (const data of parseSseChunk(text, state)) {
            const wire = mapServeEventToWire(data);
            if (!wire) continue;
            for (const h of this._handlers) {
              try {
                h(wire);
              } catch {
                /* handler errors must not kill the loop */
              }
            }
          }
        }
      } catch (e) {
        if (this._abort?.signal.aborted) break;
        await new Promise((r) => setTimeout(r, backoff));
        backoff = Math.min(backoff * 2, 8_000);
      }
    }
    this._sseTask = null;
  }

  dispose() {
    this._handlers.clear();
    try {
      this._abort?.abort();
    } catch {
      /* ignore */
    }
    this._abort = null;
    this._sseTask = null;
  }
}

/**
 * Factory for scripts/tests.
 * @param {{ baseUrl: string, token: string, fetchImpl?: typeof fetch }} opts
 */
export function createHttpSseHost(opts) {
  return new HttpSseHost(opts);
}
