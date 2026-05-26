// Tauri API 兼容别名桥接器 (Tauri Bridge for Web/Mobile)
// 双模式：Mock 模式（Vite 开发） + Server 模式（真实 CLI 后端）

type UnlistenFn = () => void;
type EventCallback<T = any> = (event: { payload: T; event: string }) => void;

// 事件监听中心
const listeners = new Map<string, Set<EventCallback>>();
let eventIdCounter = 0;
let currentTurn = 1;

// 模式检测
const modeMeta = document.querySelector('meta[name="reasonix-mode"]');
const rawMode = modeMeta?.getAttribute("content") ?? "";
const isServerMode = rawMode !== "" && rawMode !== "__REASONIX_MODE__";
const MODE = isServerMode ? "server" : "mock";

/** Web vs. native dispatcher hint — `true` whenever the dashboard is served by the CLI server, false in the Tauri desktop wrapper where native dialogs work. */
export const isWebRuntime = isServerMode;

console.log(`[tauri-bridge] mode=${MODE}${isServerMode ? ` mode="${rawMode}"` : ""}`);

// 事件广播
function broadcast(eventName: string, payload: any) {
  const callbacks = listeners.get(eventName);
  if (callbacks) {
    for (const cb of callbacks) {
      cb({ payload, event: eventName });
    }
  }
}

function emitEvent(event: Record<string, any>) {
  broadcast("rpc:event", { data: JSON.stringify(event) });
}

// SSE 连接（Server 模式）
let sse: EventSource | null = null;
let sseTurnStarted = false;
let sseReconnectAttempts = 0;
const SSE_MAX_RECONNECT_ATTEMPTS = 10;
const SSE_RECONNECT_BASE_DELAY = 1000;

let cliDisconnected = false;

function notifyCliStatus(connected: boolean) {
  if (cliDisconnected !== connected) {
    cliDisconnected = !connected;
    if (!connected) {
      emitEvent({ type: "status", text: "正在重连…", tabId: "tab-1" });
    } else {
      emitEvent({ type: "status", text: "连接已恢复", tabId: "tab-1" });
    }
  }
}

// SSE DashboardEvent → IncomingEvent 转换
function sseToIncoming(ev: any): Record<string, any>[] {
  const results: Record<string, any>[] = [];

  switch (ev.kind) {
    case "assistant_delta": {
      if (!sseTurnStarted) {
        sseTurnStarted = true;
        currentTurn++;
        results.push({
          type: "model.turn.started",
          tabId: "tab-1",
          id: ev.id,
          turn: currentTurn,
          model: "deepseek-reasoner",
        });
      }
      if (ev.contentDelta) {
        results.push({
          type: "model.delta",
          tabId: "tab-1",
          channel: "content",
          text: ev.contentDelta,
          turn: currentTurn,
        });
      }
      if (ev.reasoningDelta) {
        results.push({
          type: "model.delta",
          tabId: "tab-1",
          channel: "reasoning",
          text: ev.reasoningDelta,
          turn: currentTurn,
        });
      }
      break;
    }
    case "assistant_final": {
      // Loop's `assistant_final` fires per model iteration (mid-turn it
      // precedes tool dispatch); the real turn end is signaled by
      // `busy-change` (busy=false), handled below.
      results.push({
        type: "model.final",
        tabId: "tab-1",
        turn: currentTurn,
        content: ev.text ?? "",
        reasoningContent: ev.reasoning ?? "",
        usage: ev.usage ?? undefined,
        costUsd: ev.costUsd ?? undefined,
      });
      break;
    }
    case "tool_start": {
      results.push({
        type: "tool.preparing",
        tabId: "tab-1",
        turn: currentTurn,
        callId: ev.id,
        name: ev.toolName,
      });
      results.push({
        type: "tool.intent",
        tabId: "tab-1",
        turn: currentTurn,
        callId: ev.id,
        name: ev.toolName,
        args: ev.args ?? "",
      });
      break;
    }
    case "tool": {
      results.push({
        type: "tool.result",
        tabId: "tab-1",
        turn: currentTurn,
        callId: ev.id,
        name: ev.toolName,
        output: ev.content,
        ok: true,
      });
      break;
    }
    case "user": {
      results.push({
        type: "user.message",
        tabId: "tab-1",
        id: ev.id,
        turn: currentTurn,
        text: ev.text,
      });
      break;
    }
    case "busy-change": {
      if (!ev.busy && sseTurnStarted) {
        results.push({ type: "$turn_complete", tabId: "tab-1" });
        sseTurnStarted = false;
      }
      break;
    }
    case "modal-up": {
      const m = ev.modal;
      if (m?.kind === "shell") {
        results.push({
          type: "$confirm_required",
          tabId: "tab-1",
          id: m.id ?? ++eventIdCounter,
          kind: "run_command",
          command: m.command,
        });
      } else if (m?.kind === "choice") {
        results.push({
          type: "$choice_required",
          tabId: "tab-1",
          id: m.id ?? ++eventIdCounter,
          question: m.question,
          options: m.options ?? [],
          allowCustom: m.allowCustom ?? false,
        });
      } else if (m?.kind === "plan") {
        results.push({
          type: "$plan_required",
          tabId: "tab-1",
          id: m.id ?? ++eventIdCounter,
          plan: m.plan ?? "",
          summary: m.summary,
          steps: m.steps,
        });
      } else if (m?.kind === "checkpoint") {
        results.push({
          type: "$checkpoint_required",
          tabId: "tab-1",
          id: m.id ?? ++eventIdCounter,
          stepId: m.stepId,
          title: m.title,
          result: m.result,
          notes: m.notes,
          completed: m.completed,
          total: m.total,
        });
      }
      break;
    }
    case "modal-down": {
      // Sync close from another surface (TUI or sibling tab).
      if (typeof ev.modalKind === "string") {
        results.push({ type: "$modal_dismissed", tabId: "tab-1", kind: ev.modalKind });
      }
      break;
    }
    case "warning":
    case "error": {
      results.push({
        type: "$error",
        tabId: "tab-1",
        message: ev.text,
      });
      break;
    }
    case "status": {
      results.push({
        type: "status",
        tabId: "tab-1",
        text: ev.text,
      });
      break;
    }
    case "ping":
      break; // keep-alive, no action needed
    default:
      console.warn("[tauri-bridge] unhandled SSE event kind:", ev.kind);
  }
  return results;
}

function connectSSE(): void {
  if (sse) sse.close();
  const token =
    document.querySelector('meta[name="reasonix-token"]')?.getAttribute("content") ?? "";
  const sseUrl =
    token && token !== "__REASONIX_TOKEN__"
      ? `/api/events?token=${encodeURIComponent(token)}`
      : "/api/events";
  sse = new EventSource(sseUrl);
  sse.onmessage = (msg: MessageEvent) => {
    try {
      const dashboardEvent = JSON.parse(msg.data);
      // Token 无效时显示过期提示
      if (dashboardEvent.kind === "error" && dashboardEvent.text?.includes("token")) {
        emitEvent({
          type: "$error",
          tabId: "tab-1",
          message: "链接已过期，请重新从 CLI 打开",
        });
        return;
      }
      const events = sseToIncoming(dashboardEvent);
      for (const evt of events) emitEvent(evt);
      // 成功收到事件，重置重连计数
      sseReconnectAttempts = 0;
      notifyCliStatus(true);
    } catch (err) {
      console.warn("[tauri-bridge] bad SSE event:", err);
    }
  };
  sse.onerror = () => {
    console.warn("[tauri-bridge] SSE connection lost, retrying…");
    sse?.close();
    sse = null;
    notifyCliStatus(false);

    sseReconnectAttempts++;
    if (sseReconnectAttempts > SSE_MAX_RECONNECT_ATTEMPTS) {
      emitEvent({
        type: "$error",
        tabId: "tab-1",
        message: "CLI 已停止，请重新启动",
      });
      return;
    }

    const delay = SSE_RECONNECT_BASE_DELAY * Math.pow(2, sseReconnectAttempts - 1);
    setTimeout(connectSSE, Math.min(delay, 30000));
  };

  // SSE 连接成功后，开始定期轮询 overview 更新 balance/stats
  startStatsPolling();
}

// 定期轮询 stats/balance
let statsPollTimer: ReturnType<typeof setInterval> | null = null;

function startStatsPolling(): void {
  if (statsPollTimer) clearInterval(statsPollTimer);
  // 每 5 秒轮询一次 overview 获取最新 stats/balance
  statsPollTimer = setInterval(async () => {
    try {
      const overview = await apiFetch("overview");
      if (overview?.stats) {
        const stats = overview.stats;
        // 更新 balance
        if (stats.balance) {
          const firstBalance = stats.balance[0];
          if (firstBalance) {
            emitEvent({
              type: "$balance",
              tabId: "tab-1",
              currency: firstBalance.currency,
              total: Number.parseFloat(firstBalance.total_balance) || 0,
              isAvailable: true,
            });
          }
        }
        // 更新 usage stats（缓存命中率、tokens、金额）
        const totalTokens = Math.round(stats.totalCostUsd > 0 ? stats.cacheHitRatio * 10000 : 0);
        // 通过 $settings 事件附带更新 usage 相关信息
        // 注意：usage 主要由 model.final 事件更新，这里只更新 balance
      }
    } catch {
      // 静默失败，等待下次轮询
    }
  }, 5000);
}

// REST API 辅助
async function apiFetch(endpoint: string, options?: RequestInit): Promise<any> {
  const token = document.querySelector('meta[name="reasonix-token"]')?.getAttribute("content");
  const headers: Record<string, string> = {
    ...((options?.headers as Record<string, string>) ?? {}),
  };
  if (token && token !== "__REASONIX_TOKEN__") {
    headers["x-reasonix-token"] = token;
  }
  if (options?.body && !headers["content-type"]) {
    headers["content-type"] = "application/json";
  }
  const res = await fetch(`/api/${endpoint}`, { ...options, headers });
  if (res.status === 204) return null;
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function emitServerSettings(settings: any, overview?: any): void {
  emitEvent({
    type: "$settings",
    tabId: "tab-1",
    reasoningEffort: settings?.reasoningEffort ?? overview?.reasoningEffort ?? "high",
    editMode: settings?.editMode ?? overview?.editMode ?? "review",
    budgetUsd: settings?.budgetUsd ?? overview?.budgetUsd ?? null,
    workspaceDir: overview?.cwd ?? "",
    recentWorkspaces: [],
    model: overview?.model ?? settings?.model ?? "deepseek-v4-flash",
    editor: "code",
    webSearchEngine: settings?.webSearchEngine ?? "bing",
    subagentModels: settings?.subagentModels ?? {},
    version: overview?.version ?? "",
    baseUrl: settings?.baseUrl ?? "",
    apiKeyPrefix: settings?.apiKey ?? "",
  });
}

// 初始化 Server 状态
function emitMcpSpecsFromServer(specs: any[] | undefined, bridged: any[] | undefined): void {
  const live = new Map<string, any>();
  for (const s of bridged ?? []) {
    if (typeof s?.spec === "string") live.set(s.spec, s);
  }
  const out = (specs ?? []).map((s: any) => {
    const liveEntry = live.get(s.raw);
    return {
      raw: s.raw,
      name: s.name ?? null,
      transport: s.transport,
      summary: s.summary,
      parseError: s.parseError,
      status: liveEntry ? "connected" : s.parseError ? "failed" : "configured",
      statusReason: liveEntry ? undefined : s.parseError,
      toolCount: liveEntry?.toolCount,
    };
  });
  emitEvent({
    type: "$mcp_specs",
    tabId: "tab-1",
    specs: out,
    bridged: out.length > 0 && out.every((s) => s.status === "connected"),
  });
}

function emitSkillsFromServer(data: any): void {
  const items: any[] = [];
  const tag = (rows: any[] | undefined, scope: string) => {
    for (const r of rows ?? []) {
      items.push({
        name: r.name,
        description: r.description ?? "",
        scope,
        path: r.path ?? "",
        runAs: r.runAs ?? "inline",
        model: r.model,
      });
    }
  };
  tag(data?.builtin, "builtin");
  tag(data?.global, "global");
  tag(data?.custom, "global");
  tag(data?.project, "project");
  emitEvent({ type: "$skills", tabId: "tab-1", items });
}

function emitMemoryEntriesFromServer(entries: any[] | undefined): void {
  emitEvent({ type: "$memory", tabId: "tab-1", entries: entries ?? [] });
}

async function loadAndEmitMcp(): Promise<void> {
  try {
    const [specsResp, liveResp] = await Promise.all([apiFetch("mcp/specs"), apiFetch("mcp")]);
    emitMcpSpecsFromServer(specsResp?.specs, liveResp?.servers);
  } catch (err) {
    console.warn("[tauri-bridge] mcp fetch failed:", err);
  }
}

async function loadAndEmitSkills(): Promise<void> {
  try {
    const data = await apiFetch("skills");
    if (data) emitSkillsFromServer(data);
  } catch (err) {
    console.warn("[tauri-bridge] skills fetch failed:", err);
  }
}

async function loadAndEmitMemory(): Promise<void> {
  try {
    const data = await apiFetch("memory/entries");
    emitMemoryEntriesFromServer(data?.entries);
  } catch (err) {
    console.warn("[tauri-bridge] memory fetch failed:", err);
  }
}

async function loadAndEmitMemoryDetail(path: string): Promise<void> {
  try {
    const data = await apiFetch(`memory/read?path=${encodeURIComponent(path)}`);
    if (data?.detail) emitEvent({ type: "$memory_detail", tabId: "tab-1", detail: data.detail });
  } catch (err) {
    console.warn("[tauri-bridge] memory read failed:", err);
  }
}

async function serverInit(): Promise<void> {
  document.documentElement.dataset.web = "true";

  // 加载初始设置和会话；overview 提供 workspace 等信息
  let wsDir = "";
  try {
    const [settings, sessionsData, overview] = await Promise.all([
      apiFetch("settings"),
      apiFetch("sessions"),
      apiFetch("overview"),
    ]);
    wsDir = overview?.cwd ?? "";
    if (settings) emitServerSettings(settings, overview);
    if (sessionsData?.sessions) {
      const items = sessionsData.sessions.map((s: any) => ({
        name: s.name,
        messageCount: s.messageCount,
        mtime: new Date(s.mtime).toISOString(),
        summary: s.summary,
      }));
      emitEvent({
        type: "$sessions",
        tabId: "tab-1",
        items,
      });
    }
    // 从 overview 中提取 balance 和 stats
    const stats = overview?.stats;
    if (stats?.balance) {
      const firstBalance = stats.balance[0];
      if (firstBalance) {
        emitEvent({
          type: "$balance",
          tabId: "tab-1",
          currency: firstBalance.currency,
          total: Number.parseFloat(firstBalance.total_balance) || 0,
          isAvailable: true,
        });
      }
    }
  } catch (err) {
    console.warn("[tauri-bridge] server init failed:", err);
  }

  // Sidebar/right-rail panels — desktop pushes these on tab open; web has
  // to pull them or every panel renders empty forever (#1715).
  void Promise.all([loadAndEmitMcp(), loadAndEmitSkills(), loadAndEmitMemory()]);

  emitEvent({ type: "$ready", tabId: "tab-1" });
  emitEvent({
    type: "$tab_opened",
    tabId: "tab-1",
    workspaceDir: wsDir,
    active: true,
  });

  // 连接 SSE
  connectSSE();
}

// RPC 命令 → REST API 映射
async function serverRpc(payload: Record<string, any>): Promise<void> {
  const cmd = payload.cmd;

  switch (cmd) {
    case "user_input": {
      const result = await apiFetch("submit", {
        method: "POST",
        body: JSON.stringify({ prompt: payload.text }),
      }).catch((err) => {
        console.warn("[tauri-bridge] submit failed:", err);
        return null;
      });
      // 提交失败时通知前端保留草稿
      if (!result?.accepted) {
        emitEvent({
          type: "$error",
          tabId: "tab-1",
          message: result?.reason ?? "提交失败，请重试",
        });
      }
      break;
    }
    case "abort": {
      await apiFetch("abort", { method: "POST" }).catch(() => {});
      break;
    }
    case "session_list": {
      try {
        const data = await apiFetch("sessions");
        if (data?.sessions) {
          const items = data.sessions.map((s: any) => ({
            name: s.name,
            messageCount: s.messageCount,
            mtime: new Date(s.mtime).toISOString(),
            summary: s.summary,
          }));
          emitEvent({ type: "$sessions", tabId: "tab-1", items });
        }
      } catch {
        /* ignore */
      }
      break;
    }
    case "session_load": {
      try {
        const switchData = await apiFetch(`sessions/${encodeURIComponent(payload.name)}/switch`, {
          method: "POST",
        });
        if (!switchData?.ok) {
          console.warn("[tauri-bridge] session switch failed:", payload.name, switchData);
          break;
        }
        const data = await apiFetch(`sessions/${encodeURIComponent(payload.name)}`);
        if (!Array.isArray(data?.messages)) {
          console.warn("[tauri-bridge] session_load: GET response missing messages", data);
          break;
        }
        const raw = data.messages as any[];
        // Pre-pass: map tool-result rows by their call id so we can stitch
        // them into the assistant message that issued the call. Without
        // this, every tool segment in the replay shows "(no result)".
        const toolResults = new Map<string, { content: string; name?: string }>();
        for (const m of raw) {
          if (m?.role === "tool" && typeof m.toolCallId === "string") {
            toolResults.set(m.toolCallId, {
              content: typeof m.content === "string" ? m.content : "",
              name: typeof m.toolName === "string" ? m.toolName : undefined,
            });
          }
        }
        const messages: any[] = [];
        for (const m of raw) {
          if (m?.role === "user") {
            messages.push({ kind: "user" as const, text: m.content ?? "" });
            continue;
          }
          if (m?.role === "assistant") {
            const segments: any[] = [];
            if (typeof m.reasoning === "string" && m.reasoning.length > 0) {
              segments.push({ kind: "reasoning" as const, text: m.reasoning });
            }
            if (typeof m.content === "string" && m.content.length > 0) {
              segments.push({ kind: "text" as const, text: m.content });
            }
            if (Array.isArray(m.toolCalls)) {
              for (const tc of m.toolCalls) {
                const result = toolResults.get(tc.id);
                segments.push({
                  kind: "tool" as const,
                  callId: tc.id,
                  name: tc.name || result?.name || "tool",
                  args: tc.arguments ?? "",
                  startedAt: 0,
                  result: result?.content,
                  ok: result != null,
                  durationMs: 0,
                });
              }
            }
            messages.push({
              kind: "assistant" as const,
              turn: m.turn ?? 0,
              segments,
              pending: false,
            });
          }
          // tool-role messages are absorbed into their matching assistant segment above.
        }
        emitEvent({
          type: "$session_loaded",
          tabId: "tab-1",
          name: payload.name,
          messages,
          carryover: { totalCostUsd: 0, cacheHitTokens: 0, cacheMissTokens: 0 },
        });
      } catch (err) {
        console.warn("[tauri-bridge] session load failed:", err);
      }
      break;
    }
    case "session_delete": {
      try {
        await apiFetch(`sessions/${encodeURIComponent(payload.name)}`, { method: "DELETE" });
        // 删除后刷新列表
        const data = await apiFetch("sessions");
        if (data?.sessions) {
          const items = data.sessions.map((s: any) => ({
            name: s.name,
            messageCount: s.messageCount,
            mtime: new Date(s.mtime).toISOString(),
            summary: s.summary,
          }));
          emitEvent({ type: "$sessions", tabId: "tab-1", items });
        }
      } catch {
        /* ignore */
      }
      break;
    }
    case "new_chat": {
      try {
        const data = await apiFetch("sessions/new", { method: "POST" });
        if (data?.ok) {
          const newName = typeof data.name === "string" ? data.name : "default";
          // Treat a fresh session like a loaded-empty session so the reducer
          // resets state.currentSession + messages. `$session_empty` is for
          // "file exists but unparseable" and would render a scary error.
          emitEvent({
            type: "$session_loaded",
            tabId: "tab-1",
            name: newName,
            messages: [],
            carryover: { totalCostUsd: 0, cacheHitTokens: 0, cacheMissTokens: 0 },
          });
          const listData = await apiFetch("sessions");
          if (listData?.sessions) {
            const items = listData.sessions.map((s: any) => ({
              name: s.name,
              messageCount: s.messageCount,
              mtime: new Date(s.mtime).toISOString(),
              summary: s.summary,
            }));
            emitEvent({ type: "$sessions", tabId: "tab-1", items });
          }
        }
      } catch {
        /* fallback */
      }
      break;
    }
    case "settings_get": {
      try {
        const [settings, overview] = await Promise.all([
          apiFetch("settings"),
          apiFetch("overview"),
        ]);
        if (settings) emitServerSettings(settings, overview);
      } catch {
        /* ignore */
      }
      break;
    }
    case "settings_save": {
      try {
        await apiFetch("settings", {
          method: "POST",
          body: JSON.stringify(payload),
        });
        // 保存后主动拉取最新设置以同步 UI
        const [settings, overview] = await Promise.all([
          apiFetch("settings"),
          apiFetch("overview"),
        ]);
        if (settings) emitServerSettings(settings, overview);
      } catch {
        /* ignore */
      }
      break;
    }
    case "setup_save_key": {
      await apiFetch("settings", {
        method: "POST",
        body: JSON.stringify({ apiKey: payload.key }),
      }).catch(() => {});
      break;
    }
    case "confirm_response": {
      const kind = payload.kind === "path" ? "path" : "shell";
      await apiFetch("modal/resolve", {
        method: "POST",
        body: JSON.stringify({ kind, choice: payload.response.type }),
      }).catch(() => {});
      break;
    }
    case "choice_response": {
      const r = payload.response;
      let choice: Record<string, unknown>;
      if (r.type === "pick") choice = { kind: "pick", optionId: r.optionId };
      else if (r.type === "text") choice = { kind: "custom", text: r.text };
      else choice = { kind: "cancel" };
      await apiFetch("modal/resolve", {
        method: "POST",
        body: JSON.stringify({ kind: "choice", choice }),
      }).catch(() => {});
      break;
    }
    case "plan_response": {
      const r = payload.response;
      await apiFetch("modal/resolve", {
        method: "POST",
        body: JSON.stringify({ kind: "plan", choice: r.type, text: r.feedback }),
      }).catch(() => {});
      break;
    }
    case "checkpoint_response": {
      const r = payload.response;
      const text = r.type === "revise" ? r.feedback : undefined;
      await apiFetch("modal/resolve", {
        method: "POST",
        body: JSON.stringify({ kind: "checkpoint", choice: r.type, text }),
      }).catch(() => {});
      break;
    }
    case "revision_response": {
      const r = payload.response;
      // Server takes "accept" / "reject"; verdict uses past-participle.
      const choice =
        r.type === "accepted" ? "accept" : r.type === "rejected" ? "reject" : null;
      if (choice === null) break;
      await apiFetch("modal/resolve", {
        method: "POST",
        body: JSON.stringify({ kind: "revision", choice }),
      }).catch(() => {});
      break;
    }
    case "jobs_list": {
      try {
        const data = await apiFetch("usage");
        if (data?.jobs) {
          emitEvent({ type: "$jobs", tabId: "tab-1", items: data.jobs });
        }
      } catch {
        /* ignore */
      }
      break;
    }
    case "mention_query": {
      try {
        const data = await apiFetch(
          `files/search?q=${encodeURIComponent(payload.query)}&nonce=${payload.nonce}`,
        );
        if (data) {
          emitEvent({
            type: "$mention_results",
            tabId: "tab-1",
            nonce: payload.nonce,
            query: payload.query,
            results: data.results ?? [],
          });
        }
      } catch {
        /* ignore */
      }
      break;
    }
    case "mention_preview": {
      try {
        const data = await apiFetch(
          `file-read?path=${encodeURIComponent(payload.path)}&nonce=${payload.nonce}`,
        );
        if (data) {
          emitEvent({
            type: "$mention_preview",
            tabId: "tab-1",
            nonce: payload.nonce,
            path: payload.path,
            head: data.head ?? "",
            totalLines: data.totalLines ?? 0,
          });
        }
      } catch {
        /* ignore */
      }
      break;
    }
    // ── 桌面端特有操作（Web 版无操作或静默忽略） ──
    case "qq_status_get":
    case "qq_connect":
    case "qq_disconnect":
    case "qq_config_save":
    case "tab_activate":
    case "tab_open":
    case "tab_close":
    case "mention_picked":
    case "btw": {
      // 这些命令在 Web Server 模式下无对应 REST API，静默忽略
      break;
    }
    case "jobs_stop": {
      try {
        await apiFetch(`jobs/${payload.jobId}/stop`, { method: "POST" });
      } catch {}
      break;
    }
    case "jobs_stop_all": {
      try {
        await apiFetch("jobs/stop-all", { method: "POST" });
      } catch {}
      break;
    }
    case "compact_history": {
      try {
        await apiFetch("messages/compact", { method: "POST" });
      } catch {}
      break;
    }
    case "retry": {
      try {
        await apiFetch("submit", { method: "POST", body: JSON.stringify({ retry: true }) });
      } catch {}
      break;
    }
    case "skill_run": {
      const body: Record<string, any> = { name: payload.name };
      if (payload.args) body.args = payload.args;
      try {
        await apiFetch("skills/run", { method: "POST", body: JSON.stringify(body) });
      } catch {}
      break;
    }
    case "mcp_specs_get": {
      await loadAndEmitMcp();
      break;
    }
    case "skills_get": {
      await loadAndEmitSkills();
      break;
    }
    case "memory_get": {
      await loadAndEmitMemory();
      break;
    }
    case "memory_read": {
      if (typeof payload.path === "string") await loadAndEmitMemoryDetail(payload.path);
      break;
    }
    case "mcp_specs_add":
    case "mcp_specs_remove": {
      // MCP 操作通过 REST API 管理
      try {
        await apiFetch("mcp", {
          method: "POST",
          body: JSON.stringify({
            action: cmd === "mcp_specs_add" ? "add" : "remove",
            spec: payload.spec,
          }),
        });
        await loadAndEmitMcp();
      } catch {}
      break;
    }
    default:
      console.warn("[tauri-bridge] unhandled RPC cmd:", cmd);
  }
}

// // ═══ 导出接口（Mock + Server 双模式）
// Tauri core API
export async function invoke(cmd: string, args?: any): Promise<any> {
  console.log(`[tauri-bridge] invoke -> cmd: ${cmd}${args ? " " + JSON.stringify(args) : ""}`);

  if (MODE === "server") {
    if (cmd === "rpc_spawn") {
      serverInit().catch(console.warn);
      return Promise.resolve();
    }
    if (cmd === "rpc_send") {
      const payload = JSON.parse(args.line);
      payload.tabId = payload.tabId ?? "tab-1";
      serverRpc(payload).catch(console.warn);
      return Promise.resolve();
    }
    if (cmd === "open_in_editor") {
      console.log("[tauri-bridge] open in editor:", args);
      return Promise.resolve();
    }
    return Promise.resolve();
  }

  // Mock mode
  if (cmd === "rpc_spawn") {
    mockSetupAndReady();
    return Promise.resolve();
  }

  if (cmd === "rpc_send") {
    const payload = JSON.parse(args.line);
    if (payload.cmd === "user_input") {
      emitEvent({
        type: "user.message",
        tabId: "tab-1",
        id: Date.now(),
        ts: new Date().toISOString(),
        turn: 2,
        text: payload.text,
      });
      mockAssistantTurn(payload.text);
    } else if (payload.cmd === "session_list") {
      emitEvent({ type: "$sessions", tabId: "tab-1", items: mockSessions });
    } else if (payload.cmd === "session_load") {
      emitEvent({
        type: "$session_loaded",
        tabId: "tab-1",
        name: payload.name,
        messages: mockMessages,
        carryover: { totalCostUsd: 0.045, cacheHitTokens: 2500, cacheMissTokens: 1400 },
      });
    } else if (payload.cmd === "new_chat") {
      emitEvent({
        type: "$session_empty",
        tabId: "tab-1",
        name: "desktop-new-session",
        sizeBytes: 0,
      });
    } else if (payload.cmd === "settings_get") {
      emitEvent({ type: "$settings", tabId: "tab-1", ...mockSettings });
    }
    return Promise.resolve();
  }

  if (cmd === "open_in_editor") {
    console.log("[tauri-bridge] open in editor simulation:", args);
    return Promise.resolve();
  }

  return Promise.resolve();
}

// 2. @tauri-apps/api/event
export async function listen<T = any>(
  eventName: string,
  callback: EventCallback<T>,
): Promise<UnlistenFn> {
  let bucket = listeners.get(eventName);
  if (!bucket) {
    bucket = new Set();
    listeners.set(eventName, bucket);
  }
  bucket.add(callback);
  return () => {
    const b = listeners.get(eventName);
    if (b) b.delete(callback);
  };
}

// 3a. @tauri-apps/api/webview
export function getCurrentWebview(): any {
  return {
    onDragDropEvent: async (_callback: any): Promise<() => void> => {
      console.log("[tauri-bridge] onDragDropEvent (no-op in web)");
      return () => {};
    },
  };
}

// 3b. @tauri-apps/api/window
export function getCurrentWindow(): any {
  return {
    isMaximized: async () => false,
    minimize: async () => {
      console.log("[tauri-bridge] minimize (no-op)");
    },
    close: async () => {
      console.log("[tauri-bridge] close (no-op)");
    },
    toggleMaximize: async () => {
      console.log("[tauri-bridge] toggleMaximize (no-op)");
    },
    listen:
      async (_event: string, _callback: any): Promise<() => void> =>
      () => {},
    label: "main",
  };
}

// 4. @tauri-apps/plugin-dialog
export async function open(options?: any): Promise<any> {
  console.log("[tauri-bridge] dialog open:", options);
  return Promise.resolve("");
}

export async function save(options?: any): Promise<any> {
  console.log("[tauri-bridge] dialog save:", options);
  return Promise.resolve("");
}

// 5. @tauri-apps/plugin-opener
export async function openUrl(url: string): Promise<void> {
  console.log(`[tauri-bridge] open url -> ${url}`);
  window.open(url, "_blank");
  return Promise.resolve();
}

export async function openPath(path: string): Promise<void> {
  console.log(`[tauri-bridge] open path -> ${path}`);
  return Promise.resolve();
}

// 6. @tauri-apps/plugin-process
export async function relaunch(): Promise<void> {
  console.log("[tauri-bridge] process relaunch (reload page)");
  window.location.reload();
  return Promise.resolve();
}

// // ═══ Mock 数据（仅 Vite 开发模式使用）
//
const mockSessions = [
  {
    name: "desktop-20260520-1",
    messageCount: 8,
    mtime: new Date().toISOString(),
    summary: "项目工程模块重构及自适应 UI 设计",
  },
  {
    name: "desktop-20260519-2",
    messageCount: 14,
    mtime: new Date(Date.now() - 86400000).toISOString(),
    summary: "编写 WebSocket 桥接与 RPC 行协议对接逻辑",
  },
  {
    name: "desktop-20260518-3",
    messageCount: 4,
    mtime: new Date(Date.now() - 172800000).toISOString(),
    summary: "测试移动端 TextArea 与 Dynamic Viewport",
  },
];

const mockSettings = {
  reasoningEffort: "high",
  editMode: "review",
  budgetUsd: null,
  workspaceDir: "",
  recentWorkspaces: [],
  model: "deepseek-v4-flash",
  version: "0.47.2",
};

const mockMessages: any[] = [
  { kind: "user", text: "你好 Reasonix，帮我列出这个项目的主要技术栈以及前端架构体系。" },
  {
    kind: "assistant",
    turn: 1,
    segments: [
      { kind: "reasoning", text: "用户询问项目的技术栈和前端架构。" },
      { kind: "text", text: "你好！**DeepSeek-Reasonix** 是一个以 DeepSeek 为内核的智能代码助手…" },
    ],
    pending: false,
  },
];

function mockAssistantTurn(_promptText: string) {
  emitEvent({ type: "status", text: "DeepSeek R1 思考中...", tabId: "tab-1" });
  setTimeout(() => {
    emitEvent({
      type: "model.turn.started",
      tabId: "tab-1",
      id: Date.now(),
      turn: 2,
      model: "deepseek-reasoner",
      reasoningEffort: "high",
    });
  }, 600);
  // reasoning deltas
  const lines = ["分析用户的输入内容。\n", "用户要求提供自适应 UI 重构的验证指令。\n"];
  let delay = 1200;
  lines.forEach((line) => {
    setTimeout(() => {
      emitEvent({ type: "model.delta", tabId: "tab-1", channel: "reasoning", text: line, turn: 2 });
    }, delay);
    delay += 800;
  });
  // tool call
  setTimeout(() => {
    emitEvent({ type: "tool.preparing", tabId: "tab-1", name: "list_dir", callId: "call_12345" });
  }, delay);
  delay += 500;
  setTimeout(() => {
    emitEvent({
      type: "tool.intent",
      tabId: "tab-1",
      name: "list_dir",
      args: JSON.stringify({ DirectoryPath: "d:/AI/workspace/dashboard" }),
      callId: "call_12345",
    });
  }, delay);
  delay += 1000;
  setTimeout(() => {
    emitEvent({
      type: "tool.result",
      tabId: "tab-1",
      name: "list_dir",
      ok: true,
      output: JSON.stringify([
        { name: "package.json", sizeBytes: 864 },
        { name: "src", isDir: true },
      ]),
      callId: "call_12345",
    });
  }, delay);
  delay += 800;
  // text response
  const chunks = [
    "您的 dashboard 目录结构已确认。在**独立 Mock 开发预览阶段**，\n",
    "您可以使用 `npm run dev` 启动 Vite 开发服务，\n",
    "配合 Chrome 设备模拟器或真实手机进行移动端自适应效果验证。\n",
    "手机端支持侧滑拉出会话抽屉，输入框紧贴虚拟键盘。",
  ];
  chunks.forEach((chunk) => {
    setTimeout(() => {
      emitEvent({ type: "model.delta", tabId: "tab-1", channel: "content", text: chunk, turn: 2 });
    }, delay);
    delay += 500;
  });
  setTimeout(() => {
    emitEvent({
      type: "model.final",
      tabId: "tab-1",
      turn: 2,
      content: chunks.join(""),
      reasoningContent: lines.join(""),
      costUsd: 0.002,
    });
    emitEvent({ type: "$turn_complete", tabId: "tab-1" });
  }, delay + 400);
}

function mockSetupAndReady() {
  document.documentElement.dataset.web = "true";
  setTimeout(() => emitEvent({ type: "$ready", tabId: "tab-1" }), 100);
  setTimeout(() => {
    emitEvent({
      type: "$tab_opened",
      tabId: "tab-1",
      workspaceDir: "d:\\AI\\workspace",
      active: true,
    });
  }, 150);
  setTimeout(() => emitEvent({ type: "$settings", tabId: "tab-1", ...mockSettings }), 200);
  setTimeout(() => emitEvent({ type: "$sessions", tabId: "tab-1", items: mockSessions }), 350);
}
