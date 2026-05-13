import { describe, expect, it, vi } from "vitest";
import { McpClient } from "../src/mcp/client.js";
import type { McpTransport } from "../src/mcp/stdio.js";
import type { JsonRpcMessage } from "../src/mcp/types.js";

abstract class StubTransport implements McpTransport {
  protected closed = false;
  protected readonly queue: JsonRpcMessage[] = [];
  protected readonly waiters: Array<(m: JsonRpcMessage | null) => void> = [];

  abstract send(msg: JsonRpcMessage): Promise<void>;

  async *messages(): AsyncIterableIterator<JsonRpcMessage> {
    while (true) {
      if (this.queue.length > 0) {
        yield this.queue.shift()!;
        continue;
      }
      if (this.closed) return;
      const next = await new Promise<JsonRpcMessage | null>((resolve) => {
        this.waiters.push(resolve);
      });
      if (next === null) return;
      yield next;
    }
  }

  async close(): Promise<void> {
    this.closed = true;
    while (this.waiters.length > 0) this.waiters.shift()!(null);
  }
}

class HangingSendTransport extends StubTransport {
  async send(_msg: JsonRpcMessage): Promise<void> {
    return new Promise(() => {});
  }
}

class RejectingSendTransport extends StubTransport {
  async send(_msg: JsonRpcMessage): Promise<void> {
    throw new Error("transport send failed");
  }
}

class SilentServerTransport extends StubTransport {
  async send(_msg: JsonRpcMessage): Promise<void> {}
}

describe("McpClient.request() timeout/no-crash", () => {
  const shortTimeoutMs = 50;

  it("hung send still rejects with timeout", async () => {
    const transport = new HangingSendTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: shortTimeoutMs,
    });
    await expect(client.initialize()).rejects.toThrow(/timed out/);
    await client.close();
  });

  it("hung-send timeout does not emit unhandledRejection", async () => {
    const transport = new HangingSendTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: shortTimeoutMs,
    });
    const handler = vi.fn();
    process.on("unhandledRejection", handler);
    try {
      await expect(client.initialize()).rejects.toThrow(/timed out/);
      await new Promise((r) => setTimeout(r, shortTimeoutMs + 50));
      expect(handler).not.toHaveBeenCalled();
    } finally {
      process.off("unhandledRejection", handler);
      await client.close();
    }
  });

  it("rejecting send rejects with the send error, not timeout", async () => {
    const transport = new RejectingSendTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: 60_000,
    });
    await expect(client.initialize()).rejects.toThrow("transport send failed");
    await client.close();
  });

  it("rejecting send clears the armed timeout (no late orphan rejection)", async () => {
    const transport = new RejectingSendTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: shortTimeoutMs,
    });
    const handler = vi.fn();
    process.on("unhandledRejection", handler);
    try {
      await expect(client.initialize()).rejects.toThrow("transport send failed");
      await new Promise((r) => setTimeout(r, shortTimeoutMs + 100));
      expect(handler).not.toHaveBeenCalled();
    } finally {
      process.off("unhandledRejection", handler);
      await client.close();
    }
  });

  it("normal silent-server timeout still works", async () => {
    const transport = new SilentServerTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: shortTimeoutMs,
    });
    await expect(client.initialize()).rejects.toThrow(/timed out/);
    await client.close();
  });

  it("silent-server timeout does not emit unhandledRejection", async () => {
    const transport = new SilentServerTransport();
    const client = new McpClient({
      transport,
      requestTimeoutMs: shortTimeoutMs,
    });
    const handler = vi.fn();
    process.on("unhandledRejection", handler);
    try {
      await expect(client.initialize()).rejects.toThrow(/timed out/);
      await new Promise((r) => setTimeout(r, shortTimeoutMs + 100));
      expect(handler).not.toHaveBeenCalled();
    } finally {
      process.off("unhandledRejection", handler);
      await client.close();
    }
  });
});
