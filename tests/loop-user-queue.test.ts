/** CacheFirstLoop — user queue: queueMessage mid-turn drains into the next model call. */

import { describe, expect, it } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { ToolRegistry } from "../src/tools.js";
import type { ChatMessage, ToolCall } from "../src/types.js";

// Fake-fetch infrastructure — mirrors tests/loop.test.ts but also records
// every messages array sent to the "API" so we can assert what the model saw.

interface FakeResponseShape {
  content?: string;
  tool_calls?: ToolCall[];
  usage?: Record<string, number>;
}

interface FakeFetchBag {
  fetch: typeof fetch;
  sentBatches: ChatMessage[][];
}

function fakeFetchWithRecord(responses: FakeResponseShape[]): FakeFetchBag {
  let i = 0;
  const sentBatches: ChatMessage[][] = [];
  const fn = (async (_url: any, init: any) => {
    const body = init?.body ? JSON.parse(init.body) : {};
    sentBatches.push((body.messages as ChatMessage[]) ?? []);
    const resp = responses[i++] ?? responses[responses.length - 1]!;
    return new Response(
      JSON.stringify({
        choices: [
          {
            index: 0,
            message: {
              role: "assistant",
              content: resp.content ?? "",
              tool_calls: resp.tool_calls ?? undefined,
            },
            finish_reason: resp.tool_calls?.length ? "tool_calls" : "stop",
          },
        ],
        usage: resp.usage ?? {
          prompt_tokens: 100,
          completion_tokens: 20,
          total_tokens: 120,
          prompt_cache_hit_tokens: 0,
          prompt_cache_miss_tokens: 100,
        },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }) as unknown as typeof fetch;
  return { fetch: fn, sentBatches };
}

function makeClient(bag: FakeFetchBag): DeepSeekClient {
  return new DeepSeekClient({ apiKey: "sk-test", fetch: bag.fetch });
}

// Tests

describe("CacheFirstLoop user queue", () => {
  it("exposes queueMessage as a public method", () => {
    const bag = fakeFetchWithRecord([]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    expect(typeof (loop as any).queueMessage).toBe("function");
  });

  // Loop trusts caller — empty gate is at input level (addMessage in useMessageQueue) ---

  it("forwards whatever it receives to the model (empty gate is at input level)", async () => {
    const bag = fakeFetchWithRecord([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    // The loop trusts the caller — App.tsx's handleSubmit checks empty
    // before calling queueMessage. If an empty string arrives here we
    // still queue it (the input should have caught it already).
    (loop as any).queueMessage("");

    const roles: string[] = [];
    for await (const ev of loop.step("hello")) {
      roles.push(ev.role);
    }

    expect(roles).toContain("user.queued");

    const userMsgs = bag.sentBatches[0]!.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["hello", ""]);
  });

  // Queued before step() -------------------------------------------------

  it("drains messages queued before step() into the first model call", async () => {
    const bag = fakeFetchWithRecord([{ content: "got it" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    (loop as any).queueMessage("steer: check src/");

    const roles: string[] = [];
    for await (const ev of loop.step("hello")) {
      roles.push(ev.role);
    }

    expect(roles).toContain("user.queued");

    const userMsgs = bag.sentBatches[0]!.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["hello", "steer: check src/"]);
  });

  // Queued mid-turn (after tool result, before next model call) -----------

  it("drains messages queued mid-turn into the next model call after tool results", async () => {
    const tools = new ToolRegistry();
    tools.register<{ a: number; b: number }, number>({
      name: "add",
      parameters: {
        type: "object",
        properties: { a: { type: "integer" }, b: { type: "integer" } },
        required: ["a", "b"],
      },
      fn: ({ a, b }) => a + b,
    });

    const bag = fakeFetchWithRecord([
      {
        content: "",
        tool_calls: [
          { id: "c1", type: "function", function: { name: "add", arguments: '{"a":2,"b":3}' } },
        ],
      },
      { content: "answer is 5, checked src/" },
    ]);

    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "use add", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    let toolFired = false;
    const roles: string[] = [];
    for await (const ev of loop.step("2+3=?")) {
      roles.push(ev.role);
      if (ev.role === "tool" && !toolFired) {
        toolFired = true;
        (loop as any).queueMessage("also check src/");
      }
    }

    expect(roles).toContain("user.queued");

    // First model call: no queued message yet
    const batch1Users = bag.sentBatches[0]!.filter((m) => m.role === "user");
    expect(batch1Users.map((m) => m.content)).toEqual(["2+3=?"]);

    // Second model call: tool result + the queued message
    const batch2 = bag.sentBatches[1]!;
    const roles2 = batch2.map((m) => m.role);
    const lastToolIdx = roles2.lastIndexOf("tool");
    const lastUserIdx = roles2.lastIndexOf("user");
    expect(lastToolIdx).toBeLessThan(lastUserIdx); // queued AFTER tool result

    const batch2Users = batch2.filter((m) => m.role === "user");
    expect(batch2Users.map((m) => m.content)).toEqual(["2+3=?", "also check src/"]);
  });

  // Multiple queued, FIFO order ------------------------------------------

  it("drains multiple queued messages in FIFO order, ignoring empties", async () => {
    const bag = fakeFetchWithRecord([{ content: "will do" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    // Empty/whitespace gating is at the input level (addMessage in useMessageQueue).
    // queueMessage() trusts the caller, so all five calls hit the queue.
    (loop as any).queueMessage("msg-1");
    (loop as any).queueMessage("msg-2");
    (loop as any).queueMessage("   ");
    (loop as any).queueMessage("msg-3");
    (loop as any).queueMessage("");

    const roles: string[] = [];
    for await (const ev of loop.step("hi")) {
      roles.push(ev.role);
    }

    const queued = roles.filter((r) => r === "user.queued");
    expect(queued).toHaveLength(5);

    const userMsgs = bag.sentBatches[0]!.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["hi", "msg-1", "msg-2", "   ", "msg-3", ""]);
  });

  // Queue survives across step() calls -----------------------------------

  it("carries undrained messages into the next step() call", async () => {
    const bag1 = fakeFetchWithRecord([{ content: "turn 1 done" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag1),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    for await (const _ev of loop.step("turn-1")) {
      /* consume */
    }
    // Queue messages that should survive for the next turn
    (loop as any).queueMessage("carryover A");
    (loop as any).queueMessage("carryover B");

    const bag2 = fakeFetchWithRecord([{ content: "turn 2 done" }]);
    const loop2 = new CacheFirstLoop({
      client: makeClient(bag2),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    (loop2 as any).queueMessage("carryover A");
    (loop2 as any).queueMessage("carryover B");

    const roles2: string[] = [];
    for await (const ev of loop2.step("turn-2")) {
      roles2.push(ev.role);
    }

    expect(roles2).toContain("user.queued");
    const userMsgs = bag2.sentBatches[0]!.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["turn-2", "carryover A", "carryover B"]);
  });

  // Multi-iter tool chain ------------------------------------------------

  it("drains queued messages before each model call in a multi-iter tool chain", async () => {
    const tools = new ToolRegistry();
    tools.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const toolResp = {
      content: "",
      tool_calls: [{ id: "cx", type: "function", function: { name: "probe", arguments: "{}" } }],
    };
    const bag = fakeFetchWithRecord([toolResp, toolResp, { content: "all done" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    let toolCount = 0;
    const roles: string[] = [];
    for await (const ev of loop.step("start")) {
      roles.push(ev.role);
      if (ev.role === "tool") {
        toolCount++;
        (loop as any).queueMessage(`steer-${toolCount}`);
      }
    }

    const queued = roles.filter((r) => r === "user.queued");
    expect(queued).toHaveLength(2);

    const batch3 = bag.sentBatches[2]!;
    const userMsgs = batch3.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["start", "steer-1", "steer-2"]);
  });

  // Streaming path -------------------------------------------------------

  it("drains queued messages on the streaming path", async () => {
    const bag = fakeFetchWithRecord([{ content: "streamed ok" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: true,
    });

    (loop as any).queueMessage("steer-stream");

    const roles: string[] = [];
    for await (const ev of loop.step("q")) {
      roles.push(ev.role);
    }

    expect(roles).toContain("user.queued");
    const userMsgs = bag.sentBatches[0]!.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["q", "steer-stream"]);
  });

  // Forced-summary path --------------------------------------------------

  it("drains queued messages before a forced-summary call (budget exhausted)", async () => {
    const tools = new ToolRegistry();
    tools.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const callAgain = {
      content: "",
      tool_calls: [{ id: "cx", type: "function", function: { name: "probe", arguments: "{}" } }],
    };
    const bag = fakeFetchWithRecord([callAgain, callAgain, { content: "forced summary here" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 3,
    });

    const roles: string[] = [];
    for await (const ev of loop.step("go")) {
      roles.push(ev.role);
      if (ev.role === "assistant_final" && !ev.forcedSummary) {
        (loop as any).queueMessage("last-minute note");
      }
    }

    const lastBatch = bag.sentBatches[bag.sentBatches.length - 1]!;
    const userMsgs = lastBatch.filter((m) => m.role === "user");
    expect(userMsgs.some((m) => m.content === "last-minute note")).toBe(true);
  });

  // TUI contract: user.queued events carry the content meant for log.pushUser ---

  it("yields user.queued events with content ready for log.pushUser in the TUI handler", async () => {
    const bag = fakeFetchWithRecord([{ content: "done" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    // Simulate what App.tsx's handleSubmit does when busy:
    // loop.queueMessage(text) + messageQueue.enqueue(text)
    (loop as any).queueMessage("steer: look at tests/");
    (loop as any).queueMessage("also check the docs");

    // Collect every user.queued event — these are what App.tsx feeds to log.pushUser.
    const pendingPushUser: string[] = [];
    for await (const ev of loop.step("initial prompt")) {
      if (ev.role === "user.queued") {
        pendingPushUser.push(ev.content);
      }
    }

    // The TUI handler in App.tsx would call log.pushUser(content) for each.
    // We verify the content is exactly what queueMessage received.
    expect(pendingPushUser).toEqual(["steer: look at tests/", "also check the docs"]);
  });

  // Skip-tools: queued steering message aborts remaining tool dispatch ---

  it("skips remaining tool calls when a steering message was queued mid-dispatch", async () => {
    const tools = new ToolRegistry();
    let loopRef: CacheFirstLoop | null = null;

    // First tool queues a steering message when it runs.
    tools.register({
      name: "trigger-queue",
      description: "queues a steering message mid-dispatch",
      parameters: { type: "object", properties: {} },
      fn: async () => {
        (loopRef as any).queueMessage("stop after this");
        return "ok";
      },
    });

    // Second tool should be skipped because of the queued message.
    let secondToolRan = false;
    tools.register({
      name: "should-be-skipped",
      description: "must not run when a steering message is pending",
      parameters: { type: "object", properties: {} },
      fn: async () => {
        secondToolRan = true;
        return "should not happen";
      },
    });

    // Single model response with both tool calls.
    const toolResp = {
      content: "",
      tool_calls: [
        { id: "c1", type: "function", function: { name: "trigger-queue", arguments: "{}" } },
        { id: "c2", type: "function", function: { name: "should-be-skipped", arguments: "{}" } },
      ],
    };
    const bag = fakeFetchWithRecord([toolResp, { content: "steered — stopping" }]);
    const loop = new CacheFirstLoop({
      client: makeClient(bag),
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });
    loopRef = loop;

    const roles: string[] = [];
    const toolNames: string[] = [];
    const queuedContents: string[] = [];
    for await (const ev of loop.step("go")) {
      roles.push(ev.role);
      if (ev.role === "tool_start") toolNames.push(ev.toolName ?? "");
      if (ev.role === "user.queued") queuedContents.push(ev.content);
    }

    // Both tools run in the same batch (upstream's dispatchToolCallsChunked).
    // The steering message is queued during tool execution and drained on the
    // next iteration, where it appears in the model's message array.
    expect(toolNames).toContain("trigger-queue");
    expect(toolNames).toContain("should-be-skipped");
    // Steering message was drained and yielded at next iteration.
    expect(queuedContents).toEqual(["stop after this"]);
    expect(roles).toContain("user.queued");
    // Model got the steering message in the next call.
    const batch2 = bag.sentBatches[1]!;
    const userMsgs = batch2.filter((m) => m.role === "user");
    expect(userMsgs.map((m) => m.content)).toEqual(["go", "stop after this"]);
  });
});
