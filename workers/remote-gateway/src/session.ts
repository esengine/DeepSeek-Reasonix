import type { Env, RemoteCapability } from "./env";
import { controllerMessage, controllerPresence, parseDeviceReply } from "./protocol";

const MAX_MESSAGE_BYTES = 64 * 1024;
const MAX_CONTROLLERS = 4;

interface SocketAttachment {
  role: "device" | "controller";
  userId: number;
  scopes: RemoteCapability[];
  connectionId: string | null;
  expiresAt: number;
}

function messageBytes(message: string): number {
  return new TextEncoder().encode(message).byteLength;
}

function connectionId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export class RemoteSession {
  constructor(
    private readonly state: DurableObjectState,
    private readonly env: Env,
  ) {}

  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("upgrade")?.toLowerCase() !== "websocket") {
      return new Response("WebSocket upgrade required", { status: 426 });
    }
    const role = request.headers.get("x-reasonix-role");
    const userId = Number(request.headers.get("x-reasonix-user-id"));
    if ((role !== "device" && role !== "controller") || !Number.isSafeInteger(userId) || userId < 1) {
      return new Response("Invalid admission", { status: 401 });
    }
    const scopes = (request.headers.get("x-reasonix-scopes") ?? "")
      .split(",")
      .filter(Boolean) as RemoteCapability[];
    const expiresAt = Number(request.headers.get("x-reasonix-expires-at"));
    if (!Number.isSafeInteger(expiresAt) || expiresAt <= Date.now()) {
      return new Response("Invalid admission expiry", { status: 401 });
    }

    const devices = this.state.getWebSockets("device");
    const controllers = this.state.getWebSockets("controller");
    const existing = [...devices, ...controllers];
    const ownerMismatch = existing.some((socket) =>
      (socket.deserializeAttachment() as SocketAttachment | null)?.userId !== userId);
    if (ownerMismatch) return new Response("Session owner mismatch", { status: 403 });
    if (role === "controller" && controllers.length >= MAX_CONTROLLERS) {
      return new Response("Too many controllers", { status: 429 });
    }

    const pair = new WebSocketPair();
    const client = pair[0];
    const server = pair[1];
    const attachment: SocketAttachment = {
      role,
      userId,
      scopes,
      connectionId: role === "controller" ? connectionId() : null,
      expiresAt,
    };
    server.serializeAttachment(attachment);
    this.state.acceptWebSocket(server, [role]);

    if (role === "device") {
      for (const oldDevice of devices) oldDevice.close(4001, "Device reconnected");
      for (const controller of controllers) {
        const peer = controller.deserializeAttachment() as SocketAttachment | null;
        if (peer?.connectionId) {
          server.send(controllerPresence("controller_connected", peer.connectionId, peer.scopes));
        }
      }
    } else if (attachment.connectionId) {
      for (const device of devices) {
        device.send(controllerPresence("controller_connected", attachment.connectionId, attachment.scopes));
      }
    }
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(socket: WebSocket, message: string | ArrayBuffer): Promise<void> {
    if (typeof message !== "string") {
      socket.close(1003, "Binary messages are not supported");
      return;
    }
    if (messageBytes(message) > MAX_MESSAGE_BYTES) {
      socket.close(1009, "Message too large");
      return;
    }
    const sender = socket.deserializeAttachment() as SocketAttachment | null;
    if (!sender) {
      socket.close(1008, "Missing session identity");
      return;
    }
    if (sender.expiresAt <= Date.now()) {
      socket.close(4003, "Admission expired");
      return;
    }
    if (sender.role === "controller") {
      if (!sender.connectionId) {
        socket.close(1008, "Missing connection identity");
        return;
      }
      for (const device of this.state.getWebSockets("device")) {
        const peer = device.deserializeAttachment() as SocketAttachment | null;
        if (peer?.userId === sender.userId && device.readyState === WebSocket.OPEN) {
          device.send(controllerMessage(sender.connectionId, sender.scopes, message));
        }
      }
      return;
    }

    const reply = parseDeviceReply(message);
    if (!reply) {
      socket.close(1008, "A directed reply is required");
      return;
    }
    for (const controller of this.state.getWebSockets("controller")) {
      const peer = controller.deserializeAttachment() as SocketAttachment | null;
      if (peer?.userId === sender.userId && peer.connectionId === reply.to && controller.readyState === WebSocket.OPEN) {
        controller.send(reply.payload);
      }
    }
  }

  async webSocketClose(socket: WebSocket, code: number, reason: string, wasClean: boolean): Promise<void> {
    const attachment = socket.deserializeAttachment() as SocketAttachment | null;
    if (attachment?.role === "controller" && attachment.connectionId) {
      for (const device of this.state.getWebSockets("device")) {
        device.send(controllerPresence("controller_disconnected", attachment.connectionId, attachment.scopes));
      }
    }
    socket.close(code, wasClean ? reason : "Connection closed");
  }
}
