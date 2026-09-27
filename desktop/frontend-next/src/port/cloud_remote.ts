import type { HubPort } from "./hub";
import { SseHub } from "./hub";
import { t } from "../i18n";

const ACCOUNT = (import.meta.env.VITE_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");
const RELAY = (import.meta.env.VITE_REMOTE_GATEWAY || "wss://remote.reasonix.io").replace(/\/$/, "");
const REQUEST_TIMEOUT_MS = 30_000;
let connectionEnded = false;
const connectionEndedListeners = new Set<(reason: string) => void>();

function announceClosed(reason = "") {
  connectionEnded = true;
  for (const listener of connectionEndedListeners) listener(reason);
}

export const remoteConnectionEnded = () => connectionEnded;
export const onRemoteConnectionEnded = (listener: (reason: string) => void) => {
  connectionEndedListeners.add(listener);
  return () => { connectionEndedListeners.delete(listener); };
};

interface RemoteDevice {
  id: string;
  publicKey: string;
  capabilities: string[];
  revokedAt?: string | null;
}

interface PendingResponse {
  chunks: Map<number, string>;
  resolve: (value: Response) => void;
  reject: (reason: unknown) => void;
  timer: ReturnType<typeof setTimeout>;
  status?: number;
  contentType?: string;
  etag?: string;
}

function bytesToBase64(bytes: Uint8Array) {
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function base64ToBytes(value: string) {
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - value.length % 4) % 4);
  const binary = atob(padded);
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

function concatChunks(chunks: Map<number, string>) {
  const parts = [...chunks.entries()].sort(([a], [b]) => a - b).map(([, value]) => base64ToBytes(value));
  const size = parts.reduce((total, part) => total + part.length, 0);
  const joined = new Uint8Array(size);
  let offset = 0;
  for (const part of parts) {
    joined.set(part, offset);
    offset += part.length;
  }
  return joined;
}

async function encryptedChannel(device: RemoteDevice, socket: WebSocket) {
  const pair = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]);
  const deviceKey = await crypto.subtle.importKey("raw", base64ToBytes(device.publicKey), { name: "X25519" }, false, []);
  const secret = await crypto.subtle.deriveBits({ name: "X25519", public: deviceKey }, pair.privateKey, 256);
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const material = await crypto.subtle.importKey("raw", secret, "HKDF", false, ["deriveKey"]);
  const info = new TextEncoder().encode(`reasonix-remote-v1|${device.id}`);
  const key = await crypto.subtle.deriveKey(
    { name: "HKDF", hash: "SHA-256", salt, info },
    material,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
  const publicKey = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  socket.send(JSON.stringify({
    v: 1, type: "hello", publicKey: bytesToBase64(publicKey), salt: bytesToBase64(salt),
  }));
  return {
    async seal(value: unknown) {
      const nonce = crypto.getRandomValues(new Uint8Array(12));
      const plain = new TextEncoder().encode(JSON.stringify(value));
      const ciphertext = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData: info }, key, plain);
      return JSON.stringify({ v: 1, nonce: bytesToBase64(nonce), ciphertext: bytesToBase64(new Uint8Array(ciphertext)) });
    },
    async open(payload: string) {
      const envelope = JSON.parse(payload) as { v: number; nonce: string; ciphertext: string };
      if (envelope.v !== 1) throw new Error("Unsupported remote encryption version.");
      const plain = await crypto.subtle.decrypt(
        { name: "AES-GCM", iv: base64ToBytes(envelope.nonce), additionalData: info },
        key,
        base64ToBytes(envelope.ciphertext),
      );
      return JSON.parse(new TextDecoder().decode(plain)) as Record<string, unknown>;
    },
  };
}

function nextMessage(socket: WebSocket, timeout = 10_000): Promise<string> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(t("远程 Studio 暂无响应，请检查电脑是否在线后重试。")));
    }, timeout);
    const cleanup = () => {
      clearTimeout(timer);
      socket.removeEventListener("message", receive);
      socket.removeEventListener("close", closed);
    };
    const receive = (event: MessageEvent) => { cleanup(); resolve(String(event.data)); };
    const closed = () => { cleanup(); reject(new Error(t("远程连接已断开，请重新连接。"))); };
    socket.addEventListener("message", receive);
    socket.addEventListener("close", closed);
  });
}

class RemoteTransport {
  private readonly pending = new Map<string, PendingResponse>();

  constructor(
    private readonly socket: WebSocket,
    private readonly channel: Awaited<ReturnType<typeof encryptedChannel>>,
  ) {
    socket.addEventListener("message", (event) => void this.receive(String(event.data)));
    socket.addEventListener("close", (event) => {
      this.failAll(new Error(t("远程连接已断开，请重新连接。")));
      announceClosed(event.reason);
    });
  }

  async fetch(request: Request): Promise<Response> {
    if (this.socket.readyState !== WebSocket.OPEN) throw new Error(t("远程连接已断开，请重新连接。"));
    const id = crypto.randomUUID();
    const body = new Uint8Array(await request.arrayBuffer());
    const response = new Promise<Response>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(t("远程 Studio 暂无响应，请检查电脑是否在线后重试。")));
      }, REQUEST_TIMEOUT_MS);
      this.pending.set(id, { chunks: new Map(), resolve, reject, timer });
    });
    const payload = await this.channel.seal({
      v: 1, type: "desktop.request", id, method: request.method,
      path: request.url.slice(location.origin.length), body: bytesToBase64(body),
    });
    if (this.socket.readyState !== WebSocket.OPEN) {
      const pending = this.pending.get(id);
      if (pending) {
        clearTimeout(pending.timer);
        this.pending.delete(id);
      }
      throw new Error(t("远程连接已断开，请重新连接。"));
    }
    this.socket.send(payload);
    return response;
  }

  private async receive(wire: string) {
    let message: Record<string, unknown>;
    try {
      message = await this.channel.open(wire);
    } catch {
      return;
    }
    const id = typeof message.id === "string" ? message.id : "";
    const pending = this.pending.get(id);
    if (!pending) return;
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

  private failAll(error: Error) {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }
}

export class RemoteEventSource {
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  private closed = false;
  private after: number | null = null;

  constructor(private readonly path: string) {
    queueMicrotask(() => void this.poll());
  }

  close() {
    this.closed = true;
  }

  private async poll() {
    while (!this.closed) {
      let active = false;
      try {
        const path = this.path.replace(/\/events$/, `/events/replay?lastEventId=${this.after ?? 0}`);
        const response = await fetch(path, { credentials: "same-origin" });
        if (response.ok) {
          const body = await response.json() as { frames?: Array<Record<string, unknown>>; watermark?: number };
          active = Boolean(body.frames?.length);
          if (this.after === null) {
            const frameWatermark = (body.frames ?? []).reduce(
              (latest, frame) => typeof frame.seq === "number" ? Math.max(latest, frame.seq) : latest,
              0,
            );
            this.after = typeof body.watermark === "number" ? body.watermark : frameWatermark;
          } else {
            for (const frame of body.frames ?? []) {
              const seq = typeof frame.seq === "number" ? frame.seq : 0;
              if (seq > this.after) this.after = seq;
              this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(frame) }));
            }
          }
        }
      } catch {
        // The next poll retries while the encrypted session remains open.
      }
      await new Promise((resolve) => setTimeout(resolve, active ? 200 : 1500));
    }
  }
}

async function connect(deviceId: string, nativeFetch: typeof fetch) {
  const bootstrap = (globalThis as typeof globalThis & {
    __rxRemoteBootstrap?: Promise<Response> | null;
  }).__rxRemoteBootstrap;
  if (!bootstrap) performance.mark("reasonix:remote:start");
  const issued = await (bootstrap ?? nativeFetch(`${ACCOUNT}/me/remote-grants`, {
    method: "POST", credentials: "include", headers: { "content-type": "application/json" },
    body: JSON.stringify({ targetDeviceId: deviceId, scopes: ["desktop"] }),
  }));
  if (!issued.ok) throw new Error(t("无法授权 Web Studio，请重新登录后再试。"));
  const { grant, device: target } = await issued.json() as { grant: { ticket: string }; device?: RemoteDevice };
  if (!target || target.id !== deviceId || target.revokedAt || !target.capabilities.includes("desktop")) {
    throw new Error(t("请先更新这台电脑上的 Studio，再连接。"));
  }
  performance.mark("reasonix:remote:authorized");
  const socket = new WebSocket(`${RELAY}/v1/sessions/connect`, ["reasonix.remote.v1", `reasonix.auth.${grant.ticket}`]);
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(t("连接远程 Studio 超时，请检查电脑是否在线。"))), 10_000);
    socket.addEventListener("open", () => { clearTimeout(timer); resolve(); }, { once: true });
    socket.addEventListener("error", () => { clearTimeout(timer); reject(new Error(t("远程中转服务暂时不可用，请稍后重试。"))); }, { once: true });
  });
  const readyWire = nextMessage(socket);
  const channel = await encryptedChannel(target, socket);
  const ready = await channel.open(await readyWire);
  if (ready.type !== "ready" || ready.deviceId !== target.id) throw new Error(t("远程 Studio 身份校验失败，请停止连接并检查设备。"));
  performance.mark("reasonix:remote:ready");
  performance.measure("reasonix:remote:authorize", "reasonix:remote:start", "reasonix:remote:authorized");
  performance.measure("reasonix:remote:handshake", "reasonix:remote:authorized", "reasonix:remote:ready");
  performance.measure("reasonix:remote:connect", "reasonix:remote:start", "reasonix:remote:ready");
  return new RemoteTransport(socket, channel);
}

export async function remoteHub(deviceId: string): Promise<HubPort> {
  const nativeFetch = globalThis.fetch.bind(globalThis);
  const transport = await connect(deviceId, nativeFetch);
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    return new URL(request.url).origin === location.origin ? transport.fetch(request) : nativeFetch(request);
  }) as typeof fetch;
  globalThis.EventSource = RemoteEventSource as unknown as typeof EventSource;
  return new SseHub();
}

export const remoteCodec = { bytesToBase64, base64ToBytes, concatChunks, announceClosed };
