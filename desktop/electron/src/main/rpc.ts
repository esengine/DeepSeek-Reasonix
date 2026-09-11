export const MAX_FRAME_BYTES = 64 * 1024 * 1024;

export class OversizeFrameError extends Error {
  constructor(bytes: number, limit: number) {
    super(`protocol frame of ${bytes} bytes exceeds the ${limit} byte limit`);
    this.name = "OversizeFrameError";
  }
}

export class LineDecoder {
  private chunks: Buffer[] = [];
  private buffered = 0;

  constructor(private readonly limit = MAX_FRAME_BYTES) {}

  push(chunk: Buffer): string[] {
    const lines: string[] = [];
    let start = 0;
    for (let i = 0; i < chunk.length; i++) {
      if (chunk[i] !== 0x0a) continue;
      const tail = chunk.subarray(start, i);
      const total = this.buffered + tail.length;
      if (total > this.limit) {
        this.reset();
        throw new OversizeFrameError(total, this.limit);
      }
      const line = this.chunks.length ? Buffer.concat([...this.chunks, tail]).toString("utf8") : tail.toString("utf8");
      this.reset();
      lines.push(line.endsWith("\r") ? line.slice(0, -1) : line);
      start = i + 1;
    }
    if (start < chunk.length) {
      const rest = chunk.subarray(start);
      this.buffered += rest.length;
      if (this.buffered > this.limit) {
        const bytes = this.buffered;
        this.reset();
        throw new OversizeFrameError(bytes, this.limit);
      }
      this.chunks.push(Buffer.from(rest));
    }
    return lines;
  }

  private reset(): void {
    this.chunks = [];
    this.buffered = 0;
  }
}

export class RpcError extends Error {
  constructor(readonly code: number, message: string, readonly data?: unknown) {
    super(message);
    this.name = "RpcError";
  }
}

export interface RpcTransport {
  write(line: string): void;
}

export interface RpcHandlers {
  onRequest(method: string, params: unknown): Promise<unknown>;
  onNotification(method: string, params: unknown): void;
  onProtocolError?(kind: "non-json" | "invalid" | "orphan-response", line: string): void;
}

interface Pending {
  resolve(value: unknown): void;
  reject(error: Error): void;
  timer: NodeJS.Timeout | null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export class RpcClient {
  private nextId = 1;
  private readonly pending = new Map<number, Pending>();
  private readonly decoder: LineDecoder;
  private closedWith: Error | null = null;
  readonly stats = { ignoredLines: 0, orphanResponses: 0 };

  constructor(private readonly transport: RpcTransport, private readonly handlers: RpcHandlers, limit = MAX_FRAME_BYTES) {
    this.decoder = new LineDecoder(limit);
  }

  get closed(): boolean {
    return this.closedWith !== null;
  }

  get pendingCount(): number {
    return this.pending.size;
  }

  // Feeds raw stdout bytes; throws OversizeFrameError when the stream cannot
  // be resynchronised, after rejecting everything in flight.
  feed(chunk: Buffer): void {
    let lines: string[];
    try {
      lines = this.decoder.push(chunk);
    } catch (error) {
      this.close(error instanceof Error ? error : new Error(String(error)));
      throw error;
    }
    for (const line of lines) this.dispatch(line);
  }

  request(method: string, params: unknown, timeoutMs?: number): Promise<unknown> {
    if (this.closedWith) return Promise.reject(this.closedWith);
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const timer = timeoutMs && timeoutMs > 0
        ? setTimeout(() => {
          this.pending.delete(id);
          reject(new RpcError(-32000, `${method} timed out after ${timeoutMs} ms`));
        }, timeoutMs)
        : null;
      this.pending.set(id, { resolve, reject, timer });
      try {
        this.transport.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
      } catch (error) {
        this.pending.delete(id);
        if (timer) clearTimeout(timer);
        reject(error instanceof Error ? error : new Error(String(error)));
      }
    });
  }

  notify(method: string, params: unknown): void {
    if (this.closedWith) return;
    this.transport.write(JSON.stringify({ jsonrpc: "2.0", method, params }) + "\n");
  }

  close(error: Error): void {
    if (this.closedWith) return;
    this.closedWith = error;
    for (const [id, entry] of this.pending) {
      this.pending.delete(id);
      if (entry.timer) clearTimeout(entry.timer);
      entry.reject(error);
    }
  }

  private dispatch(line: string): void {
    if (line.trim() === "") return;
    let frame: unknown;
    try {
      frame = JSON.parse(line);
    } catch {
      this.stats.ignoredLines++;
      this.handlers.onProtocolError?.("non-json", line);
      return;
    }
    if (!isRecord(frame) || frame.jsonrpc !== "2.0") {
      this.stats.ignoredLines++;
      this.handlers.onProtocolError?.("invalid", line);
      return;
    }
    if (typeof frame.method === "string") {
      if (frame.id === undefined || frame.id === null) {
        this.handlers.onNotification(frame.method, frame.params);
        return;
      }
      this.serve(frame.id as number | string, frame.method, frame.params);
      return;
    }
    if (typeof frame.id !== "number") {
      this.stats.ignoredLines++;
      this.handlers.onProtocolError?.("invalid", line);
      return;
    }
    const entry = this.pending.get(frame.id);
    if (!entry) {
      this.stats.orphanResponses++;
      this.handlers.onProtocolError?.("orphan-response", line);
      return;
    }
    this.pending.delete(frame.id);
    if (entry.timer) clearTimeout(entry.timer);
    if (isRecord(frame.error)) {
      const code = typeof frame.error.code === "number" ? frame.error.code : -32000;
      const message = typeof frame.error.message === "string" ? frame.error.message : "unknown error";
      entry.reject(new RpcError(code, message, frame.error.data));
      return;
    }
    entry.resolve(frame.result);
  }

  private serve(id: number | string, method: string, params: unknown): void {
    this.handlers.onRequest(method, params).then(
      (result) => this.reply({ jsonrpc: "2.0", id, result: result === undefined ? null : result }),
      (error: unknown) => {
        const code = error instanceof RpcError ? error.code : -32000;
        const message = error instanceof Error ? error.message : String(error);
        this.reply({ jsonrpc: "2.0", id, error: { code, message } });
      },
    );
  }

  private reply(frame: Record<string, unknown>): void {
    if (this.closedWith) return;
    try {
      this.transport.write(JSON.stringify(frame) + "\n");
    } catch {
      // The transport owner observes the broken pipe through the process exit.
    }
  }
}
