import { EventEmitter } from "node:events";
import WebSocket from "ws";

const TOKEN_URL = "https://bots.qq.com/app/getAppAccessToken";
const BASE_URL = "https://api.sgroup.qq.com";
const SANDBOX_URL = "https://sandbox.api.sgroup.qq.com";
const INTENT_C2C_GROUP = 1 << 25;
const MIN_HEARTBEAT_INTERVAL_MS = 5_000;
const MAX_HEARTBEAT_INTERVAL_MS = 60_000;
const ALLOWED_GATEWAY_HOSTS = ["api.sgroup.qq.com", "sandbox.api.sgroup.qq.com", "qq.com"];

interface QQBotConfig {
  appid: string;
  secret: string;
  sandbox?: boolean;
}

export interface C2CMessage {
  author: { user_openid: string };
  content: string;
  id: string;
  timestamp: string;
}

export class QQBot extends EventEmitter {
  private config: QQBotConfig;
  private token = "";
  private tokenExpiresAt = 0;
  private ws: WebSocket | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private seq = 0;
  private sessionId = "";
  private closed = false;
  private readyReceived = false;

  constructor(config: QQBotConfig) {
    super();
    this.config = config;
  }

  private get baseUrl(): string {
    return this.config.sandbox ? SANDBOX_URL : BASE_URL;
  }

  private sanitizeHeartbeatInterval(interval: unknown): number | null {
    if (typeof interval !== "number" || !Number.isFinite(interval)) {
      return null;
    }
    if (interval < MIN_HEARTBEAT_INTERVAL_MS) {
      return MIN_HEARTBEAT_INTERVAL_MS;
    }
    if (interval > MAX_HEARTBEAT_INTERVAL_MS) {
      return MAX_HEARTBEAT_INTERVAL_MS;
    }
    return Math.trunc(interval);
  }

  private validateGatewayUrl(rawUrl: string): string {
    const url = new URL(rawUrl);
    const trustedHost = ALLOWED_GATEWAY_HOSTS.some(
      (host) => url.hostname === host || url.hostname.endsWith(`.${host}`),
    );
    if (
      url.protocol !== "wss:" ||
      !trustedHost ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    ) {
      throw new Error(`Unexpected QQ gateway URL: ${rawUrl}`);
    }
    return url.toString();
  }

  private async ensureToken(): Promise<string> {
    if (this.token && Date.now() < this.tokenExpiresAt - 60_000) {
      return this.token;
    }
    const res = await fetch(TOKEN_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        appId: this.config.appid,
        clientSecret: this.config.secret,
      }),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`Failed to get access token (${res.status}): ${text}`);
    }
    const data = (await res.json()) as {
      access_token: string;
      expires_in: number;
    };
    this.token = data.access_token;
    this.tokenExpiresAt = Date.now() + data.expires_in * 1000;
    return this.token;
  }

  private async getGateway(): Promise<string> {
    const token = await this.ensureToken();
    const res = await fetch(`${this.baseUrl}/gateway`, {
      headers: { Authorization: `QQBot ${token}` },
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`Failed to get gateway (${res.status}): ${text}`);
    }
    const data = (await res.json()) as { url: string };
    return this.validateGatewayUrl(data.url);
  }

  private sendOp(op: number, data?: unknown) {
    if (!this.ws) return;
    this.ws.send(JSON.stringify({ op, d: data ?? {} }));
  }

  private async handlePayload(payload: {
    op: number;
    d?: Record<string, unknown>;
    s?: number;
    t?: string;
  }) {
    switch (payload.op) {
      case 10: {
        const d = payload.d as { heartbeat_interval: number } | undefined;
        this.sendOp(2, {
          token: `QQBot ${await this.ensureToken()}`,
          intents: INTENT_C2C_GROUP,
          shard: [0, 1],
        });
        const heartbeatInterval = this.sanitizeHeartbeatInterval(d?.heartbeat_interval);
        if (heartbeatInterval) {
          this.heartbeatTimer = setInterval(() => {
            this.sendOp(1, this.seq || null);
          }, heartbeatInterval);
        }
        break;
      }
      case 0: {
        if (payload.s) this.seq = payload.s;
        if (payload.t === "READY") {
          const d = payload.d as { session_id: string; user?: { id: string } };
          this.sessionId = d.session_id;
          this.readyReceived = true;
          this.emit("online");
        } else if (payload.t === "C2C_MESSAGE_CREATE") {
          this.emit("message.private", payload.d as unknown as C2CMessage);
        } else if (payload.t === "GROUP_AT_MESSAGE_CREATE") {
          this.emit("message.group", payload.d);
        }
        break;
      }
      case 7: {
        this.reconnect();
        break;
      }
      case 9: {
        this.sessionId = "";
        this.sendOp(2, {
          token: `QQBot ${await this.ensureToken()}`,
          intents: INTENT_C2C_GROUP,
          shard: [0, 1],
        });
        break;
      }
    }
  }

  private async reconnect() {
    this.cleanup();
    await this.connect();
  }

  private cleanup() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    if (this.ws) {
      this.ws.removeAllListeners();
      this.ws.close();
      this.ws = null;
    }
  }

  private async connect() {
    const gatewayUrl = await this.getGateway();
    const token = await this.ensureToken();
    this.ws = new WebSocket(gatewayUrl, {
      headers: {
        Authorization: `QQBot ${token}`,
        "X-Union-Appid": this.config.appid,
      },
    });

    this.ws.on("open", () => {
      if (this.sessionId) {
        this.sendOp(6, {
          token: `QQBot ${this.token}`,
          session_id: this.sessionId,
          seq: this.seq,
        });
      }
    });

    this.ws.on("message", (raw: WebSocket.RawData) => {
      try {
        const payload = JSON.parse(raw.toString());
        this.handlePayload(payload).catch(() => {});
      } catch {
        // ignore parse errors
      }
    });

    this.ws.on("close", () => {
      if (!this.closed) {
        if (this.readyReceived) {
          // Was online — transient disconnect, reconnect.
          console.error("QQ WebSocket reconnecting...");
          this.cleanup();
          setTimeout(() => this.reconnect(), 3000);
        } else {
          // Never received READY — authentication or network failure.
          const msg =
            "QQ WebSocket closed before authentication completed — check your appId and appSecret";
          this.emit("bot_error", msg);
          this.closed = true; // prevent further reconnect attempts
        }
      }
    });

    this.ws.on("error", (err: Error) => {
      const msg = `QQ WebSocket error: ${err.message}`;
      console.error(msg);
      this.emit("bot_error", msg);
    });
  }

  async start(): Promise<void> {
    this.closed = false;
    this.readyReceived = false;
    await this.connect();
  }

  async stop(): Promise<void> {
    this.closed = true;
    this.cleanup();
  }

  async sendPrivateMessage(openid: string, content: string, msgId?: string): Promise<void> {
    const token = await this.ensureToken();
    const body: Record<string, unknown> = {
      content,
      msg_type: 0,
    };
    if (msgId) body.msg_id = msgId;
    const res = await fetch(`${this.baseUrl}/v2/users/${encodeURIComponent(openid)}/messages`, {
      method: "POST",
      headers: {
        Authorization: `QQBot ${token}`,
        "Content-Type": "application/json",
        "X-Union-Appid": this.config.appid,
      },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const text = await res.text();
      const msg = `QQ sendPrivateMessage failed (${res.status}): ${text}`;
      throw new Error(msg);
    }
  }
}
