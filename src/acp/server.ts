/** NDJSON JSON-RPC 2.0 server — per the ACP transport spec, one JSON object per line, no embedded newlines. */

import { type Interface, createInterface } from "node:readline";
import type { Readable, Writable } from "node:stream";
import {
  ERR_INTERNAL,
  ERR_METHOD_NOT_FOUND,
  ERR_PARSE,
  type JsonRpcId,
  type JsonRpcNotification,
  type JsonRpcRequest,
  type JsonRpcResponse,
} from "./protocol.js";

export type RequestHandler<P = unknown, R = unknown> = (params: P) => Promise<R> | R;
export type NotificationHandler<P = unknown> = (params: P) => Promise<void> | void;

export interface AcpServerOptions {
  input?: Readable;
  output?: Writable;
}

export class AcpServer {
  private requestHandlers = new Map<string, RequestHandler>();
  private notificationHandlers = new Map<string, NotificationHandler>();
  private readonly output: Writable;
  private readonly rl: Interface;
  private closed = false;

  constructor(opts: AcpServerOptions = {}) {
    this.output = opts.output ?? process.stdout;
    const input = opts.input ?? process.stdin;
    this.rl = createInterface({ input });
    this.rl.on("line", (line) => {
      void this.handleLine(line);
    });
  }

  onRequest<P, R>(method: string, handler: RequestHandler<P, R>): void {
    this.requestHandlers.set(method, handler as RequestHandler);
  }

  onNotification<P>(method: string, handler: NotificationHandler<P>): void {
    this.notificationHandlers.set(method, handler as NotificationHandler);
  }

  sendNotification(method: string, params: unknown): void {
    this.write({ jsonrpc: "2.0", method, params });
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.rl.close();
  }

  /** Wait for the input stream to end. */
  done(): Promise<void> {
    return new Promise((resolve) => this.rl.once("close", () => resolve()));
  }

  private write(msg: JsonRpcRequest | JsonRpcNotification | JsonRpcResponse): void {
    this.output.write(`${JSON.stringify(msg)}\n`);
  }

  private writeError(id: JsonRpcId | null, code: number, message: string): void {
    this.write({ jsonrpc: "2.0", id, error: { code, message } });
  }

  private async handleLine(raw: string): Promise<void> {
    const line = raw.trim();
    if (!line) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch {
      this.writeError(null, ERR_PARSE, "parse error");
      return;
    }
    if (!parsed || typeof parsed !== "object") {
      this.writeError(null, ERR_PARSE, "expected JSON object");
      return;
    }
    const msg = parsed as Partial<JsonRpcRequest>;
    if (typeof msg.method === "string" && msg.id !== undefined) {
      const id = msg.id as JsonRpcId;
      const handler = this.requestHandlers.get(msg.method);
      if (!handler) {
        this.writeError(id, ERR_METHOD_NOT_FOUND, `method not found: ${msg.method}`);
        return;
      }
      try {
        const result = await handler(msg.params);
        this.write({ jsonrpc: "2.0", id, result });
      } catch (err) {
        this.writeError(id, ERR_INTERNAL, (err as Error).message);
      }
      return;
    }
    if (typeof msg.method === "string" && msg.id === undefined) {
      const handler = this.notificationHandlers.get(msg.method);
      if (!handler) return;
      try {
        await handler(msg.params);
      } catch {
        // notifications can't be replied to — log channel would help, but stderr would pollute the wire
      }
      return;
    }
    // Responses to outbound requests are ignored in stage 1 (we don't make any yet — stage 3 will).
  }
}
