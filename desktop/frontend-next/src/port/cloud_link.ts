import { t } from "../i18n";
import type { FailureDetail, RelayFailure } from "./cloud_failure";

// Relay close codes, decided by the relay and read here by number alone.
export const CLOSE_IDLE = 4408;
export const CLOSE_REAUTH_REQUIRED = 4401;
export const CLOSE_REVOKED = 4403;
export const CLOSE_DISCONNECTED_BY_DEVICE = 4004;
const CLOSE_RESYNC = 4000;

export type CloseAction = "reconnect" | "reauth" | "end";

export function closeAction(code: number): CloseAction {
  if (code === CLOSE_REAUTH_REQUIRED) return "reauth";
  if (code === CLOSE_REVOKED || code === CLOSE_DISCONNECTED_BY_DEVICE) return "end";
  // Protocol violations would repeat on a new connection.
  if (code === 1003 || code === 1008 || code === 1009) return "end";
  return "reconnect";
}

export interface RemoteEnd {
  kind: "ended" | "reauth";
  reason: string;
  failure?: FailureDetail;
}

// Why a (re)connection attempt failed, as the account service or the relay
// said it: "transient" is worth another attempt, the others are not.
export class RemoteLinkError extends Error {
  constructor(
    readonly kind: "reauth" | "ended" | "transient",
    readonly reason: string,
    readonly failure?: FailureDetail,
  ) {
    super(reason);
  }
}

export interface RemoteChannel {
  seal(value: unknown): Promise<string>;
  open(payload: string): Promise<Record<string, unknown>>;
}

export interface RemoteConnection {
  socket: WebSocket;
  channel: RemoteChannel;
  features: string[];
  instance: string;
  listen(onMessage: (data: string) => void, onClose: (code: number, reason: string) => void): void;
}

interface Pending {
  id: string;
  method: string;
  command: Record<string, unknown>;
  sentTo: RemoteConnection | null;
  chunks: Map<number, string>;
  status?: number;
  contentType?: string;
  etag?: string;
  timer?: ReturnType<typeof setTimeout>;
  resolve: (value: Response) => void;
  reject: (reason: unknown) => void;
}

export interface RemoteLinkOptions {
  requestTimeoutMs: number;
  retryDelaysMs: number[];
}

const MAX_RETRY_AFTER_MS = 2 * 60 * 1000;

const DEFAULTS: RemoteLinkOptions = {
  requestTimeoutMs: 30_000,
  retryDelaysMs: [500, 1_000, 2_000, 4_000, 8_000, 15_000],
};

function bytesToBase64(bytes: Uint8Array) {
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function base64ToBytes(value: string) {
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - value.length % 4) % 4);
  return Uint8Array.from(atob(padded), (char) => char.charCodeAt(0));
}

function concatChunks(chunks: Map<number, string>) {
  const parts = [...chunks.entries()].sort(([a], [b]) => a - b).map(([, value]) => base64ToBytes(value));
  const joined = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    joined.set(part, offset);
    offset += part.length;
  }
  return joined;
}

// Carries desktop requests over the relay and keeps doing so across relay
// reconnects. A request that never left, or one the desktop can safely see
// twice, is resent on the next connection under the same id; a state-changing
// one already sent to a desktop that cannot recognise a resend is failed
// instead, so it never runs twice.
export class RemoteLink {
  private current: RemoteConnection | null = null;
  private readonly pending = new Map<string, Pending>();
  private reconnecting = false;
  private ended = false;
  private lastHeardAt = Date.now();
  private lastCloseCode = 0;
  private sending: Promise<void> = Promise.resolve();
  private readonly options: RemoteLinkOptions;

  constructor(
    first: RemoteConnection,
    private readonly dial: () => Promise<RemoteConnection>,
    private readonly onEnd: (end: RemoteEnd) => void,
    options: Partial<RemoteLinkOptions> = {},
  ) {
    this.options = { ...DEFAULTS, ...options };
    this.attach(first);
  }

  async fetch(request: Request): Promise<Response> {
    if (this.ended) throw new Error(t("远程连接已断开，请重新连接。"));
    const id = crypto.randomUUID();
    const body = new Uint8Array(await request.arrayBuffer());
    const command = {
      v: 1, type: "desktop.request", id, method: request.method,
      path: request.url.slice(location.origin.length), body: bytesToBase64(body),
    };
    return new Promise<Response>((resolve, reject) => {
      const entry: Pending = {
        id, method: request.method.toUpperCase(), command, sentTo: null, chunks: new Map(), resolve, reject,
      };
      this.pending.set(id, entry);
      void this.send(entry);
    });
  }

  private attach(connection: RemoteConnection) {
    this.current = connection;
    this.lastHeardAt = Date.now();
    connection.listen((data) => {
      if (this.current !== connection) return;
      this.lastHeardAt = Date.now();
      void this.receive(connection, data);
    }, (code, reason) => {
      if (this.current === connection) this.lost(code, reason);
    });
  }

  // Commands are numbered as they are sealed, so they leave in sealing order.
  private send(entry: Pending): Promise<void> {
    this.sending = this.sending.then(() => this.sendNow(entry), () => this.sendNow(entry));
    return this.sending;
  }

  private async sendNow(entry: Pending) {
    const connection = this.current;
    if (!connection || connection.socket.readyState !== WebSocket.OPEN || !this.pending.has(entry.id)) return;
    const wire = await connection.channel.seal(entry.command);
    if (this.current !== connection || connection.socket.readyState !== WebSocket.OPEN || !this.pending.has(entry.id)) return;
    entry.chunks.clear();
    entry.sentTo = connection;
    clearTimeout(entry.timer);
    entry.timer = setTimeout(() => this.timedOut(entry), this.options.requestTimeoutMs);
    connection.socket.send(wire);
  }

  // A desktop that has said nothing at all for a whole request timeout has lost
  // this connection's encrypted session; one that is merely slow has not.
  private timedOut(entry: Pending) {
    if (!this.pending.has(entry.id)) return;
    if (this.current && Date.now() - this.lastHeardAt >= this.options.requestTimeoutMs) {
      const stale = this.current;
      this.lost(CLOSE_RESYNC, "Desktop stopped answering");
      try { stale.socket.close(CLOSE_RESYNC, "Resync"); } catch { /* already closing */ }
      return;
    }
    this.pending.delete(entry.id);
    entry.reject(new Error(t("远程 Studio 暂无响应，请检查电脑是否在线后重试。")));
  }

  private lost(code: number, reason: string) {
    if (this.ended) return;
    this.current = null;
    this.lastCloseCode = code;
    const action = closeAction(code);
    if (action !== "reconnect") {
      this.finish({ kind: action === "reauth" ? "reauth" : "ended", reason });
      return;
    }
    for (const entry of this.pending.values()) clearTimeout(entry.timer);
    this.settleUnanswered((sentTo) => sentTo.features.includes("replay"));
    if (!this.reconnecting) void this.reconnect();
  }

  private async reconnect() {
    this.reconnecting = true;
    let failure: FailureDetail | undefined;
    let notBefore = 0;
    try {
      for (const scheduled of this.options.retryDelaysMs) {
        await new Promise((resolve) => setTimeout(resolve, Math.max(scheduled, notBefore)));
        if (this.ended) return;
        try {
          const connection = await this.dial();
          if (this.ended) {
            connection.socket.close(1000, "Ended");
            return;
          }
          this.attach(connection);
          this.settleUnanswered((sentTo) =>
            sentTo.features.includes("replay") && sentTo.instance !== "" && sentTo.instance === connection.instance);
          for (const entry of this.pending.values()) void this.send(entry);
          return;
        } catch (error) {
          if (error instanceof RemoteLinkError && error.kind !== "transient") {
            this.finish({ kind: error.kind, reason: error.reason });
            return;
          }
          failure = error instanceof RemoteLinkError ? error.failure : undefined;
          notBefore = Math.min((failure?.retryAfterS ?? 0) * 1000, MAX_RETRY_AFTER_MS);
        }
      }
      failure ??= this.lastCloseCode === CLOSE_IDLE ? { code: "idle" satisfies RelayFailure } : undefined;
      this.finish({
        kind: "ended",
        reason: t("远程 Studio 暂无响应，请检查电脑是否在线后重试。"),
        ...(failure ? { failure } : {}),
      });
    } finally {
      this.reconnecting = false;
    }
  }

  // A state change already sent is resent only where it cannot run twice: to
  // the same desktop process, which answers a resend from its replay cache.
  private settleUnanswered(canResend: (sentTo: RemoteConnection) => boolean) {
    for (const entry of [...this.pending.values()]) {
      const idempotent = entry.method === "GET" || entry.method === "HEAD";
      if (entry.sentTo && !idempotent && !canResend(entry.sentTo)) {
        this.pending.delete(entry.id);
        entry.reject(new Error(t("连接中断，这次操作可能没有完成，请刷新后确认。")));
      }
    }
  }

  private finish(end: RemoteEnd) {
    if (this.ended) return;
    this.ended = true;
    const error = new Error(end.kind === "reauth"
      ? t("远程控制需要重新登录。")
      : t("远程连接已断开，请重新连接。"));
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer);
      entry.reject(error);
    }
    this.pending.clear();
    this.onEnd(end);
  }

  private async receive(connection: RemoteConnection, wire: string) {
    let message: Record<string, unknown>;
    try {
      message = await connection.channel.open(wire);
    } catch {
      return;
    }
    const id = typeof message.id === "string" ? message.id : "";
    const pending = this.pending.get(id);
    if (!pending || this.current !== connection) return;
    if (message.type === "error") {
      clearTimeout(pending.timer);
      this.pending.delete(id);
      pending.reject(new Error(typeof message.error === "string" ? message.error : t("远程 Studio 拒绝了这次请求。")));
      return;
    }
    if (message.type !== "desktop.response" || typeof message.index !== "number" || typeof message.body !== "string") return;
    pending.status = typeof message.status === "number" ? message.status : pending.status;
    pending.contentType = typeof message.contentType === "string" ? message.contentType : pending.contentType;
    pending.etag = typeof message.etag === "string" ? message.etag : pending.etag;
    pending.chunks.set(message.index, message.body);
    if (message.done !== true) return;
    clearTimeout(pending.timer);
    this.pending.delete(id);
    const headers = new Headers();
    if (pending.contentType) headers.set("content-type", pending.contentType);
    if (pending.etag) headers.set("etag", pending.etag);
    const status = pending.status ?? 500;
    const bytes = concatChunks(pending.chunks);
    pending.resolve(new Response(status === 204 || status === 304 ? null : bytes, { status, headers }));
  }
}

export const linkCodec = { bytesToBase64, base64ToBytes, concatChunks };
