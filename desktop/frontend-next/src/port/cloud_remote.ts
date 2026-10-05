import type { HubPort } from "./hub";
import { SseHub } from "./hub";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { classifyHandshakeFailure, failureView, retryAfterSeconds } from "./cloud_failure";
import { linkCodec, RemoteLink, RemoteLinkError, type RemoteConnection, type RemoteEnd } from "./cloud_link";

const ACCOUNT = (import.meta.env.VITE_ACCOUNTS_API || "https://id.reasonix.io").replace(/\/$/, "");
const RELAY = (import.meta.env.VITE_REMOTE_GATEWAY || "wss://remote.reasonix.io").replace(/\/$/, "");
const REMOTE_HOME = import.meta.env.VITE_REMOTE_HOME || "https://reasonix.io/remote/";
const HANDSHAKE_TIMEOUT_MS = 30_000;

// The socket errored before it opened: the browser hides the status, so the
// caller has to find out why.
export class SocketRefused extends Error {}
let connectionEnded: RemoteEnd | null = null;
const connectionEndedListeners = new Set<(end: RemoteEnd) => void>();
const { bytesToBase64, base64ToBytes, concatChunks } = linkCodec;

function announceClosed(end: RemoteEnd) {
  connectionEnded = end;
  for (const listener of connectionEndedListeners) listener(end);
}

export type { RemoteEnd };
export const remoteConnectionEnded = () => connectionEnded;
export const onRemoteConnectionEnded = (listener: (end: RemoteEnd) => void) => {
  connectionEndedListeners.add(listener);
  return () => { connectionEndedListeners.delete(listener); };
};

export function openRemoteHome() {
  location.href = REMOTE_HOME;
}

// Signing out first is what makes the sign-in page ask: it sends a visitor who
// still holds a session straight on, and that session is the one too old to
// control a computer.
export async function signInAgain(deviceId: string, nativeFetch: typeof fetch = globalThis.fetch.bind(globalThis)) {
  try {
    await nativeFetch(`${ACCOUNT}/auth/logout`, { method: "POST", credentials: "include" });
  } catch {
    // The sign-in page still works; it will just offer to continue as the old session.
  }
  const home = new URL(REMOTE_HOME);
  home.searchParams.set("device", deviceId);
  location.href = `${home.origin}/login/?next=${encodeURIComponent(home.pathname + home.search)}`;
}

interface RemoteDevice {
  id: string;
  publicKey: string;
  capabilities: string[];
  revokedAt?: string | null;
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
  // Once the desktop names this session, every command carries that name and
  // an increasing number, so a relay cannot replay a command into a later
  // session or twice into this one.
  let bound: string | null = null;
  let seq = 0;
  return {
    bind(session: string) {
      bound = session;
    },
    async seal(value: unknown) {
      const nonce = crypto.getRandomValues(new Uint8Array(12));
      const stamped = bound && value && typeof value === "object"
        ? { ...(value as Record<string, unknown>), session: bound, seq: ++seq }
        : value;
      const plain = new TextEncoder().encode(JSON.stringify(stamped));
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

function nextMessage(socket: WebSocket, timeout = HANDSHAKE_TIMEOUT_MS): Promise<string> {
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

function waitForSocketOpen(socket: WebSocket, timeout = HANDSHAKE_TIMEOUT_MS): Promise<void> {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer);
      socket.removeEventListener("open", opened);
      socket.removeEventListener("error", failed);
    };
    const opened = () => { cleanup(); resolve(); };
    const failed = () => {
      cleanup();
      reject(new SocketRefused());
    };
    const timer = setTimeout(() => {
      cleanup();
      try { socket.close(); } catch { /* The browser may reject closing a socket that never left CONNECTING. */ }
      reject(new Error(t("连接远程 Studio 超时，请检查电脑是否在线。")));
    }, timeout);
    socket.addEventListener("open", opened);
    socket.addEventListener("error", failed);
  });
}

interface ReplayAnswer {
  frames?: Array<Record<string, unknown>> | null;
  complete?: boolean;
  watermark?: number;
}

// Stands in for EventSource over the relay by polling /events/replay. A cursor
// the URL carries is where a replacement stream resumes; without one it attaches
// at the watermark, and asking past it returns no frames, not the whole log.
// Frames the SSE transport writes on its own (the watermark, a gap) are said
// here too, or the stream reads as silent to whoever watches it.
export class RemoteEventSource {
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  private closed = false;
  private after: number | null;
  private readonly replay: string;

  constructor(url: string) {
    const target = new URL(url, location.origin);
    const cursor = Number(target.searchParams.get("lastEventId") ?? "");
    this.after = target.searchParams.has("lastEventId") && Number.isSafeInteger(cursor) && cursor >= 0 ? cursor : null;
    this.replay = `${target.pathname}/replay`;
    queueMicrotask(() => void this.poll());
  }

  close() {
    this.closed = true;
  }

  private say(frame: Record<string, unknown>) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(frame) }));
  }

  private async poll() {
    while (!this.closed) {
      let active = false;
      try {
        const response = await fetch(`${this.replay}?lastEventId=${this.after ?? Number.MAX_SAFE_INTEGER}`, { credentials: "same-origin" });
        if (response.ok && !this.closed) {
          const body = await response.json() as ReplayAnswer;
          const watermark = typeof body.watermark === "number" ? body.watermark : null;
          const attaching = this.after === null;
          const frames = attaching ? [] : body.frames ?? [];
          active = frames.length > 0;
          if (this.after === null) this.after = watermark ?? 0;
          else if (body.complete === false) {
            const from = typeof frames[0]?.seq === "number" ? frames[0].seq : watermark ?? this.after;
            this.say({ kind: "stream_gap", seq: from });
          }
          for (const frame of frames) {
            const seq = typeof frame.seq === "number" ? frame.seq : 0;
            if (seq > this.after) this.after = seq;
            this.say(frame);
          }
          if (!active && watermark !== null) this.say({ kind: "stream_watermark", seq: watermark });
        }
      } catch {
        // The next poll retries while the encrypted session remains open.
      }
      await new Promise((resolve) => setTimeout(resolve, active ? 200 : 1500));
    }
  }
}

async function grantFailure(issued: Response): Promise<RemoteLinkError> {
  const body = await issued.json().catch(() => null) as { error?: { code?: string } } | null;
  const code = body?.error?.code;
  if (issued.status === 401 || code === "remote_reauth_required") {
    return new RemoteLinkError("reauth", t("远程控制需要重新登录。"));
  }
  if (issued.status === 404 || code === "device_not_found") {
    return new RemoteLinkError("ended", t("这台电脑已从账号中移除。"));
  }
  if (issued.status === 429) {
    const detail = { code: "rate_limited" as const, retryAfterS: retryAfterSeconds(issued.headers.get("retry-after")) };
    return new RemoteLinkError("transient", failureView(detail).body, detail);
  }
  return new RemoteLinkError("transient", t("无法授权 Web Studio，请重新登录后再试。"));
}

async function connect(deviceId: string, nativeFetch: typeof fetch, useBootstrap: boolean): Promise<RemoteConnection> {
  const scope = globalThis as typeof globalThis & { __rxRemoteBootstrap?: Promise<Response> | null };
  const bootstrap = useBootstrap ? scope.__rxRemoteBootstrap : null;
  scope.__rxRemoteBootstrap = null;
  if (!bootstrap) performance.mark("reasonix:remote:start");
  let issued: Response;
  try {
    issued = await (bootstrap ?? nativeFetch(`${ACCOUNT}/me/remote-grants`, {
      method: "POST", credentials: "include", headers: { "content-type": "application/json" },
      body: JSON.stringify({ targetDeviceId: deviceId, scopes: ["desktop"] }),
    }));
  } catch {
    const detail = { code: navigator.onLine === false ? "offline" as const : "unreachable" as const };
    throw new RemoteLinkError("transient", failureView(detail).body, detail);
  }
  if (!issued.ok) throw await grantFailure(issued);
  const { grant, device: target } = await issued.json() as { grant: { ticket: string }; device?: RemoteDevice };
  if (!target || target.id !== deviceId || target.revokedAt || !target.capabilities.includes("desktop")) {
    throw new RemoteLinkError("ended", t("请先更新这台电脑上的 Studio，再连接。"));
  }
  performance.mark("reasonix:remote:authorized");
  const socket = new WebSocket(`${RELAY}/v1/sessions/connect`, ["reasonix.remote.v1", `reasonix.auth.${grant.ticket}`]);
  try {
    await waitForSocketOpen(socket);
    const readyWire = nextMessage(socket);
    const channel = await encryptedChannel(target, socket);
    const ready = await channel.open(await readyWire);
    if (ready.type !== "ready" || ready.deviceId !== target.id) {
      throw new RemoteLinkError("ended", t("远程 Studio 身份校验失败，请停止连接并检查设备。"));
    }
    performance.mark("reasonix:remote:ready");
    performance.measure("reasonix:remote:authorize", "reasonix:remote:start", "reasonix:remote:authorized");
    performance.measure("reasonix:remote:handshake", "reasonix:remote:authorized", "reasonix:remote:ready");
    performance.measure("reasonix:remote:connect", "reasonix:remote:start", "reasonix:remote:ready");
    const features = Array.isArray(ready.features) ? ready.features.filter((item): item is string => typeof item === "string") : [];
    if (typeof ready.session === "string" && ready.session) channel.bind(ready.session);
    const instance = typeof ready.instance === "string" ? ready.instance : "";
    return {
      socket, channel, features, instance,
      listen(onMessage, onClose) {
        socket.addEventListener("message", (event) => onMessage(String(event.data)));
        socket.addEventListener("close", (event) => onClose(event.code, event.reason));
      },
    };
  } catch (error) {
    try { socket.close(1000, "Handshake failed"); } catch { /* never opened */ }
    if (error instanceof RemoteLinkError) throw error;
    if (error instanceof SocketRefused) {
      const detail = await classifyHandshakeFailure({
        relay: RELAY, online: () => navigator.onLine !== false, fetch: nativeFetch,
      }).catch(() => ({ code: "unknown" as const }));
      throw new RemoteLinkError("transient", failureView(detail).body, detail);
    }
    throw new RemoteLinkError("transient", reason(error));
  }
}

export async function remoteHub(deviceId: string): Promise<HubPort> {
  const nativeFetch = globalThis.fetch.bind(globalThis);
  let first: RemoteConnection;
  try {
    first = await connect(deviceId, nativeFetch, true);
  } catch (error) {
    if (error instanceof RemoteLinkError && error.kind === "reauth") {
      await signInAgain(deviceId, nativeFetch);
      return new Promise<never>(() => {});
    }
    throw error;
  }
  const link = new RemoteLink(first, () => connect(deviceId, nativeFetch, false), announceClosed);
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    return new URL(request.url).origin === location.origin ? link.fetch(request) : nativeFetch(request);
  }) as typeof fetch;
  globalThis.EventSource = RemoteEventSource as unknown as typeof EventSource;
  return new SseHub();
}

export const remoteCodec = {
  bytesToBase64, base64ToBytes, concatChunks, announceClosed,
  waitForSocketOpen, handshakeTimeoutMS: HANDSHAKE_TIMEOUT_MS,
};
