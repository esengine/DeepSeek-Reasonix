/** ACP (Agent Client Protocol) server — NDJSON framing + JSON-RPC method dispatch. */

import { PassThrough } from "node:stream";
import { describe, expect, it } from "vitest";
import { dispatchKernelEvent, toolKindFor } from "../src/acp/dispatch.js";
import {
  ACP_PROTOCOL_VERSION,
  type ContentBlock,
  ERR_METHOD_NOT_FOUND,
  ERR_PARSE,
  flattenPrompt,
} from "../src/acp/protocol.js";
import { AcpServer } from "../src/acp/server.js";
import type { Event as KernelEvent } from "../src/core/events.js";

function makePair(): {
  server: AcpServer;
  send: (msg: unknown) => void;
  reads: () => string[];
} {
  const input = new PassThrough();
  const output = new PassThrough();
  const collected: string[] = [];
  output.on("data", (chunk: Buffer) => {
    for (const line of chunk.toString("utf8").split("\n")) {
      const trimmed = line.trim();
      if (trimmed) collected.push(trimmed);
    }
  });
  const server = new AcpServer({ input, output });
  return {
    server,
    send: (msg) => input.write(`${JSON.stringify(msg)}\n`),
    reads: () => collected.slice(),
  };
}

function wait(ms = 10): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("AcpServer — NDJSON framing", () => {
  it("dispatches a request to a registered handler and writes a JSON-RPC response", async () => {
    const { server, send, reads } = makePair();
    server.onRequest<{ x: number }, { y: number }>("math/double", (p) => ({ y: p.x * 2 }));
    send({ jsonrpc: "2.0", id: 1, method: "math/double", params: { x: 21 } });
    await wait();
    expect(reads()).toEqual([JSON.stringify({ jsonrpc: "2.0", id: 1, result: { y: 42 } })]);
    server.close();
  });

  it("rejects unknown methods with -32601 method not found", async () => {
    const { server, send, reads } = makePair();
    send({ jsonrpc: "2.0", id: 2, method: "nope" });
    await wait();
    const reply = JSON.parse(reads()[0] ?? "{}");
    expect(reply.error?.code).toBe(ERR_METHOD_NOT_FOUND);
    expect(reply.id).toBe(2);
    server.close();
  });

  it("returns a parse-error response for malformed JSON", async () => {
    const { server, reads } = makePair();
    // bypass JSON.stringify — emit raw garbage
    (server as unknown as { handleLine: (l: string) => Promise<void> }).handleLine = AcpServer
      .prototype["handleLine" as keyof AcpServer] as never;
    const input = new PassThrough();
    const output = new PassThrough();
    const collected: string[] = [];
    output.on("data", (chunk: Buffer) => {
      for (const line of chunk.toString("utf8").split("\n")) {
        if (line.trim()) collected.push(line.trim());
      }
    });
    const s = new AcpServer({ input, output });
    input.write("not json\n");
    await wait();
    expect(JSON.parse(collected[0] ?? "{}").error?.code).toBe(ERR_PARSE);
    s.close();
    server.close();
    expect(reads()).toEqual([]);
  });

  it("does not respond to notifications", async () => {
    const { server, send, reads } = makePair();
    let seen: unknown = null;
    server.onNotification<{ tag: string }>("ping", (p) => {
      seen = p;
    });
    send({ jsonrpc: "2.0", method: "ping", params: { tag: "v1" } });
    await wait();
    expect(seen).toEqual({ tag: "v1" });
    expect(reads()).toEqual([]);
    server.close();
  });

  it("emits a notification verbatim on sendNotification", async () => {
    const { server, reads } = makePair();
    server.sendNotification("session/update", {
      sessionId: "s1",
      update: { sessionUpdate: "agent_message_chunk", content: { type: "text", text: "hi" } },
    });
    // small wait to let the stream flush
    await wait(5);
    const parsed = JSON.parse(reads()[0] ?? "{}");
    expect(parsed.method).toBe("session/update");
    expect(parsed.params.update.content.text).toBe("hi");
    expect(parsed.id).toBeUndefined();
    server.close();
  });

  it("returns a -32603 internal error when a handler throws", async () => {
    const { server, send, reads } = makePair();
    server.onRequest("oops", () => {
      throw new Error("kaboom");
    });
    send({ jsonrpc: "2.0", id: 9, method: "oops" });
    await wait();
    const reply = JSON.parse(reads()[0] ?? "{}");
    expect(reply.error?.message).toBe("kaboom");
    expect(reply.id).toBe(9);
    server.close();
  });
});

describe("ACP protocol helpers", () => {
  it("flattenPrompt concatenates text and resource-with-inline-text blocks", () => {
    const blocks: ContentBlock[] = [
      { type: "text", text: "analyze this" },
      {
        type: "resource",
        resource: { uri: "file:///x.py", mimeType: "text/x-python", text: "print('hi')" },
      },
      { type: "text", text: "thanks" },
    ];
    expect(flattenPrompt(blocks)).toBe("analyze this\n\nprint('hi')\n\nthanks");
  });

  it("flattenPrompt ignores image / audio / resource-without-text blocks", () => {
    const blocks: ContentBlock[] = [
      { type: "image", mimeType: "image/png", data: "AAAA" },
      {
        type: "resource",
        resource: { uri: "file:///x.bin", mimeType: "application/octet-stream" },
      },
      { type: "text", text: "only this survives" },
    ];
    expect(flattenPrompt(blocks)).toBe("only this survives");
  });

  it("ACP_PROTOCOL_VERSION pins to the spec's v1", () => {
    expect(ACP_PROTOCOL_VERSION).toBe(1);
  });
});

describe("ACP kernel-event dispatch", () => {
  function captureUpdates(): { server: AcpServer; updates: () => unknown[] } {
    const input = new PassThrough();
    const output = new PassThrough();
    const lines: string[] = [];
    output.on("data", (chunk: Buffer) => {
      for (const line of chunk.toString("utf8").split("\n")) {
        if (line.trim()) lines.push(line.trim());
      }
    });
    const server = new AcpServer({ input, output });
    return {
      server,
      updates: () =>
        lines
          .map((l) => JSON.parse(l))
          .filter((m) => m.method === "session/update")
          .map((m) => m.params.update),
    };
  }

  function kev<T extends KernelEvent["type"]>(type: T, fields: Partial<KernelEvent>): KernelEvent {
    return {
      id: 1,
      ts: "2026-05-12T00:00:00Z",
      turn: 1,
      type,
      ...fields,
    } as KernelEvent;
  }

  it("model.delta on content channel emits agent_message_chunk", async () => {
    const { server, updates } = captureUpdates();
    dispatchKernelEvent(
      server,
      "s1",
      kev("model.delta", { channel: "content", text: "hello" } as never),
    );
    await wait(5);
    expect(updates()).toEqual([
      { sessionUpdate: "agent_message_chunk", content: { type: "text", text: "hello" } },
    ]);
    server.close();
  });

  it("model.delta on reasoning channel emits agent_thought_chunk", async () => {
    const { server, updates } = captureUpdates();
    dispatchKernelEvent(
      server,
      "s1",
      kev("model.delta", { channel: "reasoning", text: "thinking…" } as never),
    );
    await wait(5);
    expect(updates()).toEqual([
      { sessionUpdate: "agent_thought_chunk", content: { type: "text", text: "thinking…" } },
    ]);
    server.close();
  });

  it("tool.preparing emits a pending tool_call with the right kind classification", async () => {
    const { server, updates } = captureUpdates();
    dispatchKernelEvent(
      server,
      "s1",
      kev("tool.preparing", { callId: "tc-1", name: "read_file" } as never),
    );
    await wait(5);
    expect(updates()).toEqual([
      {
        sessionUpdate: "tool_call",
        toolCallId: "tc-1",
        title: "read_file",
        kind: "read",
        status: "pending",
      },
    ]);
    server.close();
  });

  it("tool.intent emits in_progress then a tool_call carrying parsed rawInput", async () => {
    const { server, updates } = captureUpdates();
    dispatchKernelEvent(
      server,
      "s1",
      kev("tool.intent", {
        callId: "tc-2",
        name: "write_file",
        args: '{"path":"a.ts","content":"x"}',
      } as never),
    );
    await wait(5);
    const seen = updates();
    expect(seen).toHaveLength(2);
    expect(seen[0]).toEqual({
      sessionUpdate: "tool_call_update",
      toolCallId: "tc-2",
      status: "in_progress",
    });
    expect(seen[1]).toMatchObject({
      sessionUpdate: "tool_call",
      toolCallId: "tc-2",
      kind: "edit",
      status: "in_progress",
      rawInput: { path: "a.ts", content: "x" },
    });
    server.close();
  });

  it("tool.result emits completed with content; failed when ok=false", async () => {
    const { server, updates } = captureUpdates();
    dispatchKernelEvent(
      server,
      "s1",
      kev("tool.result", {
        callId: "tc-3",
        ok: true,
        output: "42",
        durationMs: 7,
      } as never),
    );
    dispatchKernelEvent(
      server,
      "s1",
      kev("tool.result", {
        callId: "tc-4",
        ok: false,
        output: "ENOENT",
        durationMs: 1,
      } as never),
    );
    await wait(5);
    const seen = updates();
    expect(seen).toHaveLength(2);
    expect(seen[0]).toMatchObject({
      sessionUpdate: "tool_call_update",
      toolCallId: "tc-3",
      status: "completed",
      content: [{ type: "content", content: { type: "text", text: "42" } }],
    });
    expect(seen[1]).toMatchObject({
      toolCallId: "tc-4",
      status: "failed",
      content: [{ type: "content", content: { type: "text", text: "ENOENT" } }],
    });
    server.close();
  });

  it("very long tool outputs are clipped with a truncation suffix", async () => {
    const { server, updates } = captureUpdates();
    const big = "x".repeat(8000 + 200);
    dispatchKernelEvent(
      server,
      "s1",
      kev("tool.result", { callId: "tc-5", ok: true, output: big, durationMs: 1 } as never),
    );
    await wait(5);
    const seen = updates();
    const text = (seen[0] as { content: Array<{ content: { text: string } }> }).content[0]?.content
      .text as string;
    expect(text.length).toBeGreaterThan(0);
    expect(text.length).toBeLessThan(big.length);
    expect(text).toContain("more chars truncated");
    server.close();
  });

  it("toolKindFor classifies known tool names into ACP kinds", () => {
    expect(toolKindFor("read_file")).toBe("read");
    expect(toolKindFor("glob")).toBe("read");
    expect(toolKindFor("write_file")).toBe("edit");
    expect(toolKindFor("multi_edit")).toBe("edit");
    expect(toolKindFor("search_content")).toBe("search");
    expect(toolKindFor("run_command")).toBe("execute");
    expect(toolKindFor("run_background")).toBe("execute");
    expect(toolKindFor("totally_made_up_tool")).toBe("other");
  });
});

describe("ACP initialize handshake (end-to-end via the server)", () => {
  it("implements initialize → returns protocolVersion + agentCapabilities + agentInfo", async () => {
    const { server, send, reads } = makePair();
    // The CLI command wires the initialize handler; mirror its shape here for an isolated test
    // so the wire contract is covered without spinning up the loop.
    server.onRequest("initialize", () => ({
      protocolVersion: ACP_PROTOCOL_VERSION,
      agentCapabilities: {
        loadSession: false,
        promptCapabilities: { image: false, audio: false, embeddedContext: true },
        mcpCapabilities: { http: false, sse: false },
      },
      agentInfo: { name: "reasonix", title: "Reasonix", version: "0.0.0-test" },
      authMethods: [],
    }));
    send({
      jsonrpc: "2.0",
      id: 0,
      method: "initialize",
      params: { protocolVersion: 1, clientCapabilities: { fs: { readTextFile: true } } },
    });
    await wait();
    const reply = JSON.parse(reads()[0] ?? "{}");
    expect(reply.id).toBe(0);
    expect(reply.result.protocolVersion).toBe(1);
    expect(reply.result.agentInfo.name).toBe("reasonix");
    expect(reply.result.authMethods).toEqual([]);
    server.close();
  });
});
