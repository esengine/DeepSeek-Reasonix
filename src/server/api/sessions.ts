import { existsSync, readFileSync } from "node:fs";
import { deleteSession, listSessions, sessionPath } from "../../memory/session.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface SessionToolCall {
  id: string;
  name: string;
  /** Raw arguments string the model emitted (typically JSON). */
  arguments: string;
}

interface SessionMessage {
  role: string;
  content?: string;
  /** Assistant `reasoning_content` (R1 / V4 thinking). */
  reasoning?: string;
  /** Assistant tool_calls — emitted alongside `content` for tool-call turns. */
  toolCalls?: SessionToolCall[];
  /** Tool-result message: the call id this row answers. */
  toolCallId?: string;
  /** Tool-result message: the tool name (legacy `tool_name` or `name`). */
  toolName?: string;
}

function parseTranscript(path: string, maxBytes = 4 * 1024 * 1024): SessionMessage[] {
  // Cap reads at 4 MB so a runaway session file (rare but possible)
  // doesn't tie up the server. The `head` of a long session is the
  // useful part; we surface a `truncated` flag in the response.
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    return [];
  }
  if (raw.length > maxBytes) raw = raw.slice(0, maxBytes);
  const out: SessionMessage[] = [];
  for (const line of raw.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      const rec = JSON.parse(line) as Record<string, unknown>;
      const role = typeof rec.role === "string" ? rec.role : "unknown";
      const msg: SessionMessage = { role };
      if (typeof rec.content === "string") msg.content = rec.content;
      else if (rec.content !== undefined) msg.content = JSON.stringify(rec.content);
      if (typeof rec.reasoning_content === "string") msg.reasoning = rec.reasoning_content;
      if (Array.isArray(rec.tool_calls)) {
        const calls: SessionToolCall[] = [];
        for (const c of rec.tool_calls as Array<Record<string, unknown>>) {
          const fn = (c?.function ?? {}) as Record<string, unknown>;
          const id = typeof c?.id === "string" ? c.id : "";
          const name = typeof fn.name === "string" ? fn.name : "";
          const args = typeof fn.arguments === "string" ? fn.arguments : "";
          if (id || name) calls.push({ id, name, arguments: args });
        }
        if (calls.length > 0) msg.toolCalls = calls;
      }
      if (typeof rec.tool_call_id === "string") msg.toolCallId = rec.tool_call_id;
      else if (typeof rec.toolCallId === "string") msg.toolCallId = rec.toolCallId;
      if (typeof rec.tool_name === "string") msg.toolName = rec.tool_name;
      else if (typeof rec.toolName === "string") msg.toolName = rec.toolName;
      else if (typeof rec.name === "string" && role === "tool") msg.toolName = rec.name;
      out.push(msg);
    } catch {
      /* skip malformed line — same rule as the rest of Reasonix's JSONL readers */
    }
  }
  return out;
}

export async function handleSessions(
  method: string,
  rest: string[],
  _body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  // Listing — workspace-scoped when the CLI knows its cwd. Without this,
  // every subagent transcript and every other-workspace session lands in the
  // sidebar; users have reported 10 000+ entries in `~/.reasonix/sessions/`.
  if (method === "GET" && rest.length === 0) {
    const workspaceFilter = ctx.getCurrentCwd?.();
    const sessions = workspaceFilter ? listSessions({ workspaceFilter }) : listSessions();
    const currentName = ctx.getSessionName?.() ?? null;
    return {
      status: 200,
      body: {
        sessions: sessions.map((s) => ({
          name: s.name,
          path: s.path,
          size: s.size,
          messageCount: s.messageCount,
          mtime: s.mtime.getTime(),
          summary: s.meta?.summary,
        })),
        currentSession: currentName,
        canSwitch: Boolean(ctx.switchSession),
      },
    };
  }

  // New session — mints a fresh session by calling switchSession(undefined).
  // We echo the new session name back so the dashboard can update its own
  // currentSession (and the URL via #1586's mirror effect) without having to
  // diff the listing.
  if (method === "POST" && rest.length === 1 && rest[0] === "new") {
    if (!ctx.switchSession) {
      return {
        status: 503,
        body: { error: "live session swap requires an attached CLI session." },
      };
    }
    const result = ctx.switchSession(undefined);
    if (!result.ok) return { status: 500, body: { error: result.reason } };
    return { status: 200, body: { ok: true, name: ctx.getSessionName?.() ?? null } };
  }

  if (rest.length === 0) {
    return { status: 405, body: { error: `method ${method} not supported on /sessions` } };
  }

  // Single-session detail / switch / delete. URL-decode in case the name
  // had spaces / CJK (sanitizeName allows them).
  const name = decodeURIComponent(rest[0]!);
  const path = sessionPath(name);
  const currentName = ctx.getSessionName?.() ?? null;

  if (method === "POST" && rest[1] === "switch") {
    if (!ctx.switchSession) {
      return {
        status: 503,
        body: { error: "live session swap requires an attached CLI session." },
      };
    }
    if (!existsSync(path)) return { status: 404, body: { error: `no such session: ${name}` } };
    const result = ctx.switchSession(name);
    if (!result.ok) return { status: 500, body: { error: result.reason } };
    return { status: 200, body: { ok: true } };
  }

  if (method === "DELETE") {
    if (rest.length !== 1) {
      return { status: 405, body: { error: `method ${method} not supported on this path` } };
    }
    // Refuse to delete the currently-attached session — the live process
    // still has the file open for append, and deleting it would resurrect
    // an empty file on the next message.
    if (currentName && name === currentName) {
      return {
        status: 409,
        body: { error: "cannot delete the currently-active session — switch away first." },
      };
    }
    if (!existsSync(path)) return { status: 404, body: { error: `no such session: ${name}` } };
    const removed = deleteSession(name);
    if (!removed) return { status: 500, body: { error: `failed to delete ${name}` } };
    ctx.audit?.({ ts: Date.now(), action: "delete-session", payload: { name } });
    return { status: 200, body: { ok: true, deleted: name } };
  }

  if (method === "GET") {
    if (rest.length !== 1) {
      return { status: 405, body: { error: `method ${method} not supported on this path` } };
    }
    if (!existsSync(path)) return { status: 404, body: { error: `no such session: ${name}` } };
    const messages = parseTranscript(path);
    return {
      status: 200,
      body: { name, path, messages, messageCount: messages.length },
    };
  }

  return { status: 405, body: { error: `method ${method} not supported on this path` } };
}
