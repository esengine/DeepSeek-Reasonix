/**
 * SessionBackend is the only runtime surface the mobile UI depends on.
 * LocalBackend talks to mobilecore; RemoteBackend talks to reasonix node.
 * Runtime is immutable after create — migration always creates a new session.
 */

import {
  LOCAL_CAPABILITIES,
  MOBILE_PROTOCOL_VERSION,
  REMOTE_CAPABILITIES,
  type CreateSessionArgs,
  type MobileEnvelope,
  type SessionDescriptor,
  type SessionRuntime,
  type SnapshotPayload,
  type SubmitArgs,
  newEnvelope,
} from "../protocol/types";
import {
  isDangerousWrite,
  riskFromTool,
  type ApprovalRequest,
} from "../lib/approval";

export type SessionEventHandler = (event: unknown, seq: number) => void;

export interface SessionBackend {
  readonly runtime: SessionRuntime;
  createSession(args: CreateSessionArgs): Promise<SessionDescriptor>;
  restoreSession(id: string): Promise<SnapshotPayload>;
  submit(sessionId: string, args: SubmitArgs, requestId: string): Promise<void>;
  cancel(sessionId: string, requestId: string): Promise<void>;
  approve(
    sessionId: string,
    args: { id: string; allow: boolean; session?: boolean; persist?: boolean },
    requestId: string,
  ): Promise<void>;
  snapshot(sessionId: string): Promise<SnapshotPayload>;
  listModels(): Promise<{ id: string; label?: string }[]>;
  subscribe(sessionId: string, handler: SessionEventHandler): () => void;
}

function newId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
}

function needsDemoApproval(text: string): ApprovalRequest | null {
  const lower = text.toLowerCase();
  // Explicit demo trigger or tool-like dangerous intent.
  const forced =
    lower.includes("approve:") ||
    lower.includes("/approve") ||
    /\b(rm\s|delete\s|sudo\s|write_file|shell\s)/i.test(text) ||
    text.includes("删除") ||
    text.includes("刪除");
  if (!forced) return null;

  let tool = "shell";
  let subject = text.slice(0, 120);
  let command: string | undefined = text;
  let diff: string | undefined;
  if (/\bwrite_file\b|write\s+/i.test(text) || text.includes("写入")) {
    tool = "write_file";
    subject = "src/example.ts";
    command = undefined;
    diff = [
      "--- a/src/example.ts",
      "+++ b/src/example.ts",
      "@@ -1,3 +1,4 @@",
      " export function main() {",
      "-  return 0",
      "+  console.log('updated')",
      "+  return 1",
      " }",
    ].join("\n");
  } else if (/\bdelete\b|rm\s|删除|刪除/i.test(text)) {
    tool = "delete_file";
    subject = "tmp/scratch.log";
    command = `rm -f ${subject}`;
  }

  const risk = riskFromTool(tool, subject);
  return {
    id: newId("appr"),
    sessionId: "",
    tool,
    subject,
    reason: "Local demo: tool requires confirmation before side effects.",
    risk,
    command,
    diff,
    dangerousWrite: isDangerousWrite(risk, tool),
  };
}

/**
 * In-memory LocalBackend used until the Capacitor mobilecore plugin is wired.
 * Shape matches the Go mobilecore JSON API so the bridge swap is mechanical.
 */
export class LocalBackend implements SessionBackend {
  readonly runtime: SessionRuntime = "local";
  private sessions = new Map<string, SessionDescriptor>();
  private handlers = new Map<string, Set<SessionEventHandler>>();
  private seq = new Map<string, number>();
  private pending = new Map<
    string,
    { approvalId: string; resolve: (allow: boolean) => void }
  >();

  async createSession(args: CreateSessionArgs): Promise<SessionDescriptor> {
    if (args.runtime !== "local") {
      throw new Error("LocalBackend only creates local sessions");
    }
    const d: SessionDescriptor = {
      id: newId("local"),
      runtime: "local",
      providerRef: args.providerRef,
      capabilities: [...LOCAL_CAPABILITIES],
      revision: 1,
      lastEventSeq: 0,
      title: args.title || "Local session",
      status: "idle",
      updatedAt: new Date().toISOString(),
    };
    this.sessions.set(d.id, d);
    this.seq.set(d.id, 0);
    return d;
  }

  async restoreSession(id: string): Promise<SnapshotPayload> {
    return this.snapshot(id);
  }

  async submit(sessionId: string, args: SubmitArgs, requestId: string): Promise<void> {
    const d = this.require(sessionId);
    if (!args.text.trim()) throw new Error("text is required");
    d.status = "running";
    d.updatedAt = new Date().toISOString();
    this.emit(sessionId, { kind: "turn_started", requestId });

    const approval = needsDemoApproval(args.text);
    if (approval) {
      approval.sessionId = sessionId;
      d.status = "pending_approval";
      // Register the waiter before emit so sync subscribers can resolve immediately.
      const allowed = await new Promise<boolean>((resolve) => {
        this.pending.set(sessionId, { approvalId: approval.id, resolve });
        this.emit(sessionId, {
          kind: "approval_request",
          approval: {
            id: approval.id,
            tool: approval.tool,
            subject: approval.subject,
            reason: approval.reason,
            risk: approval.risk,
            command: approval.command,
            diff: approval.diff,
            dangerousWrite: approval.dangerousWrite,
          },
        });
      });
      if (!allowed) {
        this.emit(sessionId, {
          kind: "notice",
          level: "warn",
          text: "approval denied — turn cancelled",
        });
        this.emit(sessionId, { kind: "turn_done", requestId, err: "denied" });
        d.status = "idle";
        d.lastEventSeq = this.seq.get(sessionId) ?? 0;
        d.updatedAt = new Date().toISOString();
        return;
      }
      this.emit(sessionId, {
        kind: "tool_dispatch",
        text: `${approval.tool} ${approval.subject}`,
      });
      this.emit(sessionId, {
        kind: "tool_result",
        text: `${approval.tool} completed`,
      });
    } else {
      this.emit(sessionId, {
        kind: "notice",
        level: "info",
        text: "local backend accepted message (mobilecore stream pending)",
      });
    }

    this.emit(sessionId, { kind: "turn_done", requestId });
    d.status = "idle";
    d.lastEventSeq = this.seq.get(sessionId) ?? 0;
    d.updatedAt = new Date().toISOString();
  }

  async cancel(sessionId: string, requestId: string): Promise<void> {
    const d = this.require(sessionId);
    const p = this.pending.get(sessionId);
    if (p) {
      this.pending.delete(sessionId);
      p.resolve(false);
    }
    d.status = "idle";
    this.emit(sessionId, { kind: "notice", text: "cancel", requestId });
  }

  async approve(
    sessionId: string,
    args: { id: string; allow: boolean },
    requestId: string,
  ): Promise<void> {
    this.require(sessionId);
    const p = this.pending.get(sessionId);
    if (p && p.approvalId === args.id) {
      this.pending.delete(sessionId);
      p.resolve(args.allow);
    }
    this.emit(sessionId, {
      kind: "notice",
      text: `approval ${args.id} allow=${args.allow}`,
      requestId,
    });
  }

  async snapshot(sessionId: string): Promise<SnapshotPayload> {
    const d = this.require(sessionId);
    return {
      descriptor: { ...d },
      lastEventSeq: d.lastEventSeq,
      revision: d.revision,
      running: d.status === "running" || d.status === "pending_approval",
    };
  }

  async listModels(): Promise<{ id: string; label?: string }[]> {
    return [];
  }

  subscribe(sessionId: string, handler: SessionEventHandler): () => void {
    let set = this.handlers.get(sessionId);
    if (!set) {
      set = new Set();
      this.handlers.set(sessionId, set);
    }
    set.add(handler);
    return () => set!.delete(handler);
  }

  private require(id: string): SessionDescriptor {
    const d = this.sessions.get(id);
    if (!d) throw new Error("session not found");
    return d;
  }

  private emit(sessionId: string, event: unknown): void {
    const next = (this.seq.get(sessionId) ?? 0) + 1;
    this.seq.set(sessionId, next);
    const d = this.sessions.get(sessionId);
    if (d) d.lastEventSeq = next;
    for (const h of this.handlers.get(sessionId) ?? []) {
      h(event, next);
    }
  }
}

/**
 * RemoteBackend connects to reasonix node over the mobile WebSocket protocol.
 * Direct LAN/Tailscale and official relay share this same application protocol.
 */
export class RemoteBackend implements SessionBackend {
  readonly runtime: SessionRuntime = "remote";
  private baseUrl: string;
  private handlers = new Map<string, Set<SessionEventHandler>>();
  private lastAck = new Map<string, number>();

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  get nodeBaseUrl(): string {
    return this.baseUrl;
  }

  async createSession(args: CreateSessionArgs): Promise<SessionDescriptor> {
    if (args.runtime !== "remote") {
      throw new Error("RemoteBackend only creates remote sessions");
    }
    const env = await this.postCommand("create_session", args, {
      requestId: newId("req"),
    });
    if (env.type === "error") {
      throw new Error(errorMessage(env));
    }
    const snap = env.payload as SnapshotPayload | undefined;
    if (!snap?.descriptor) {
      throw new Error("missing snapshot descriptor");
    }
    return {
      ...snap.descriptor,
      capabilities: snap.descriptor.capabilities?.length
        ? snap.descriptor.capabilities
        : [...REMOTE_CAPABILITIES],
    };
  }

  async restoreSession(id: string): Promise<SnapshotPayload> {
    return this.snapshot(id);
  }

  async submit(sessionId: string, args: SubmitArgs, requestId: string): Promise<void> {
    const env = await this.postCommand("submit", args, { sessionId, requestId });
    if (env.type === "error") throw new Error(errorMessage(env));
    if (typeof env.ack === "number") this.lastAck.set(sessionId, env.ack);
  }

  async cancel(sessionId: string, requestId: string): Promise<void> {
    const env = await this.postCommand("cancel", {}, { sessionId, requestId });
    if (env.type === "error") throw new Error(errorMessage(env));
  }

  async approve(
    sessionId: string,
    args: { id: string; allow: boolean; session?: boolean; persist?: boolean },
    requestId: string,
  ): Promise<void> {
    const env = await this.postCommand("approve", args, { sessionId, requestId });
    if (env.type === "error") throw new Error(errorMessage(env));
  }

  async snapshot(sessionId: string): Promise<SnapshotPayload> {
    const env = await this.postCommand("snapshot", {}, { sessionId, requestId: newId("req") });
    if (env.type === "error") throw new Error(errorMessage(env));
    return env.payload as SnapshotPayload;
  }

  async listModels(): Promise<{ id: string; label?: string }[]> {
    const env = await this.postCommand("list_models", {}, { requestId: newId("req") });
    if (env.type === "error") throw new Error(errorMessage(env));
    const payload = env.payload as { models?: { id: string; label?: string }[] };
    return payload?.models ?? [];
  }

  subscribe(sessionId: string, handler: SessionEventHandler): () => void {
    let set = this.handlers.get(sessionId);
    if (!set) {
      set = new Set();
      this.handlers.set(sessionId, set);
    }
    set.add(handler);
    return () => set!.delete(handler);
  }

  private async postCommand(
    name: string,
    args: unknown,
    meta: { sessionId?: string; requestId?: string },
  ): Promise<MobileEnvelope> {
    const envelope = newEnvelope("command", {
      requestId: meta.requestId,
      sessionId: meta.sessionId,
      payload: { name, args },
    });
    const res = await fetch(`${this.baseUrl}/mobile/command`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(envelope),
    });
    if (!res.ok) {
      throw new Error(`node HTTP ${res.status}`);
    }
    const body = (await res.json()) as MobileEnvelope;
    if (body.version !== MOBILE_PROTOCOL_VERSION && body.version > MOBILE_PROTOCOL_VERSION) {
      throw new Error(`unsupported protocol version ${body.version}`);
    }
    return body;
  }
}

function errorMessage(env: MobileEnvelope): string {
  const p = env.payload as { message?: string; code?: string } | undefined;
  return p?.message || p?.code || "remote error";
}

/** Factory used by UI when creating a session after runtime selection. */
export function backendForRuntime(
  runtime: SessionRuntime,
  remoteBaseUrl?: string,
): SessionBackend {
  if (runtime === "local") return new LocalBackend();
  if (!remoteBaseUrl) {
    throw new Error("remote runtime requires node base URL");
  }
  return new RemoteBackend(remoteBaseUrl);
}
