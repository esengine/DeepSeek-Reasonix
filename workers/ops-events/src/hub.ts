import type { Env } from "./env";
import { frame, type NewEvent } from "./events";
import { HubCore } from "./hub_core";

const MAX_SOCKETS = 4;

export class EventHub implements DurableObject {
  private readonly core: HubCore;

  constructor(private readonly state: DurableObjectState, _env: Env) {
    this.core = new HubCore(state.storage as never);
    state.setWebSocketAutoResponse(new WebSocketRequestResponsePair("ping", "pong"));
  }

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/connect") return this.accept(url);
    if (url.pathname === "/push" && request.method === "POST") {
      const body = (await request.json()) as { event: NewEvent; delivery?: string; rateKey?: string };
      const result = await this.core.push(body.event, { delivery: body.delivery, rateKey: body.rateKey });
      if (result.accepted && result.event) {
        const text = frame(result.event);
        let sent = false;
        for (const socket of this.state.getWebSockets()) {
          try {
            socket.send(text);
            sent = true;
          } catch {
            // a socket that cannot receive is closed by the runtime; the event stays buffered
          }
        }
        if (sent) await this.core.markDelivered(result.event.id);
      }
      return Response.json({ accepted: result.accepted, reason: result.reason, id: result.event?.id }, { status: result.accepted ? 202 : result.reason === "rate" ? 429 : 200 });
    }
    return new Response("not found", { status: 404 });
  }

  private async accept(url: URL): Promise<Response> {
    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    const existing = this.state.getWebSockets();
    if (existing.length >= MAX_SOCKETS) existing[0]?.close(1008, "too many listeners");
    this.state.acceptWebSocket(server);
    const backlog = await this.core.replay(url.searchParams.get("since") ?? "auto");
    for (const event of backlog) server.send(frame(event));
    if (backlog.length) await this.core.markDelivered(backlog[backlog.length - 1]!.id);
    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(_socket: WebSocket, _message: string | ArrayBuffer): Promise<void> {
    // listeners only receive; anything they send is ignored
  }

  async webSocketClose(socket: WebSocket, code: number): Promise<void> {
    try {
      socket.close(code === 1005 || code === 1006 ? 1000 : code, "");
    } catch {
      // already closed
    }
  }
}
