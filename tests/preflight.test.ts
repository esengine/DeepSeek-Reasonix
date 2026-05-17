/** Preflight context-size check — local estimate + auto-compact before send when reactive compact would arrive too late. */

import { afterEach, describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { DEEPSEEK_CONTEXT_TOKENS } from "../src/telemetry/stats.js";
import { ToolRegistry } from "../src/tools.js";
import type { ChatMessage } from "../src/types.js";

interface FakeResponseShape {
  content?: string;
  usage?: Record<string, number>;
}

function fakeFetch(responses: FakeResponseShape[]): typeof fetch {
  let i = 0;
  return vi.fn(async () => {
    const resp = responses[i++] ?? responses[responses.length - 1]!;
    return new Response(
      JSON.stringify({
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: resp.content ?? "", tool_calls: undefined },
            finish_reason: "stop",
          },
        ],
        usage: resp.usage ?? {
          prompt_tokens: 100,
          completion_tokens: 10,
          total_tokens: 110,
          prompt_cache_hit_tokens: 0,
          prompt_cache_miss_tokens: 100,
        },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }) as unknown as typeof fetch;
}

function makeClient(responses: FakeResponseShape[]) {
  return new DeepSeekClient({ apiKey: "sk-test", fetch: fakeFetch(responses) });
}

describe("preflight context-size check", () => {
  const TEST_MODEL = "test-tiny-ctx";
  afterEach(() => {
    delete DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL];
  });

  it("mechanically truncates when the estimated request exceeds 95% of the context window", async () => {
    // Tiny 1000-token budget so modest content can overflow.
    DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL] = 1000;

    const fetchFn = fakeFetch([{ content: "ack" }]);
    const client = new DeepSeekClient({ apiKey: "sk-test", fetch: fetchFn });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "be brief" }),
      stream: false,
      model: TEST_MODEL,
    });

    // Seed the log with a PROPERLY paired (assistant.tool_calls ↔
    // tool) turn so buildMessages doesn't strip the tool result as
    // an orphan. The tool result is oversized enough to push the
    // preflight estimate past 95% of the 1000-token budget. Realistic
    // log-line content to avoid the tokenizer's BPE O(n²) pathological
    // path on pure-repeat inputs.
    loop.log.append({ role: "user", content: "prior request" });
    loop.log.append({
      role: "assistant",
      content: "",
      tool_calls: [{ id: "prior", type: "function", function: { name: "probe", arguments: "{}" } }],
    });
    loop.log.append({
      role: "tool",
      tool_call_id: "prior",
      content: "ERROR: step failed with trailing detail\n".repeat(500),
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("follow-up")) {
      events.push({ role: ev.role, content: ev.content });
    }

    // Preflight fires BEFORE the request, but the emergency path is
    // local-only: no summary LLM call should run before the user's call.
    const warn = events.find((e) => e.role === "warning" && /^preflight:/.test(e.content ?? ""));
    expect(warn).toBeDefined();
    expect(warn!.content).toMatch(/truncated \d+ messages/);
    expect(warn!.content).not.toMatch(/summary \d+ chars/);
    expect(fetchFn).toHaveBeenCalledTimes(1);

    // Loop still completed normally (no forced summary, no error).
    expect(events.find((e) => e.role === "error")).toBeUndefined();
    const finals = events.filter((e) => e.role === "assistant_final");
    expect(finals.length).toBe(1);
  });

  it("mechanically truncates when oversized tool-call arguments dominate the estimate", async () => {
    DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL] = 500;

    const fetchFn = fakeFetch([{ content: "ack" }]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch: fetchFn }),
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      model: TEST_MODEL,
    });

    const largeArgs = JSON.stringify({
      command: Array.from({ length: 600 }, (_, i) => `inspect-file-${i}`).join(" "),
    });
    loop.log.append({ role: "user", content: "prior request" });
    loop.log.append({
      role: "assistant",
      content: "",
      tool_calls: [
        { id: "prior", type: "function", function: { name: "probe", arguments: largeArgs } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "prior", content: "ok" });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("hi")) {
      events.push({ role: ev.role, content: ev.content });
    }

    const warn = events.find((e) => e.role === "warning" && /^preflight:/.test(e.content ?? ""));
    expect(warn).toBeDefined();
    expect(warn!.content).toMatch(/truncated \d+ messages/);
    expect(warn!.content).not.toMatch(/nothing left to truncate/);
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });

  it("warns when truncation cannot get the full request under 95%", async () => {
    DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL] = 500;

    const fetchFn = fakeFetch([{ content: "ack" }]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch: fetchFn }),
      prefix: new ImmutablePrefix({ system: "You are a careful assistant. ".repeat(300) }),
      stream: false,
      model: TEST_MODEL,
    });

    loop.log.append({ role: "user", content: "prior request" });
    loop.log.append({
      role: "assistant",
      content: "",
      tool_calls: [{ id: "prior", type: "function", function: { name: "probe", arguments: "{}" } }],
    });
    loop.log.append({
      role: "tool",
      tool_call_id: "prior",
      content: "ERROR: step failed with trailing detail\n".repeat(500),
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("hi")) {
      events.push({ role: ev.role, content: ev.content });
    }

    const warn = events.find((e) => e.role === "warning" && /^preflight:/.test(e.content ?? ""));
    expect(warn).toBeDefined();
    expect(warn!.content).toMatch(/still/);
    expect(warn!.content).toMatch(/truncat(?:ed|ing) \d+ messages/);
    expect(warn!.content).not.toMatch(/Sending/);
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });

  it("does not clear the active tool turn when a tool result cannot fit the target", async () => {
    DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL] = 500;

    const tools = new ToolRegistry();
    tools.register({
      name: "big_result",
      description: "return an oversized result",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      fn: () => "ERROR: tool output with important context\n".repeat(5000),
    });

    const payloads: Array<{ messages: ChatMessage[] }> = [];
    let calls = 0;
    const fetchFn = vi.fn(async (_url: unknown, init: RequestInit | undefined) => {
      payloads.push(JSON.parse(String(init?.body ?? "{}")) as { messages: ChatMessage[] });
      calls++;
      const message =
        calls === 1
          ? {
              role: "assistant",
              content: "",
              tool_calls: [
                {
                  id: "call-1",
                  type: "function",
                  function: { name: "big_result", arguments: "{}" },
                },
              ],
            }
          : { role: "assistant", content: "final answer", tool_calls: undefined };
      return new Response(
        JSON.stringify({
          choices: [{ index: 0, message, finish_reason: "stop" }],
          usage: {
            prompt_tokens: 100,
            completion_tokens: 10,
            total_tokens: 110,
            prompt_cache_hit_tokens: 0,
            prompt_cache_miss_tokens: 100,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch;

    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch: fetchFn }),
      prefix: new ImmutablePrefix({ system: "s" }),
      tools,
      stream: false,
      model: TEST_MODEL,
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("please use the big tool")) {
      events.push({ role: ev.role, content: ev.content });
    }

    expect(payloads).toHaveLength(2);
    expect(payloads[1]!.messages).not.toEqual([{ role: "system", content: "s" }]);
    expect(payloads[1]!.messages.some((m) => m.role === "user")).toBe(true);
    expect(payloads[1]!.messages.some((m) => m.role === "tool")).toBe(true);
    expect(loop.log.toMessages().some((m) => m.role === "user")).toBe(true);
    expect(
      events.find((e) => e.role === "warning" && /^preflight:/.test(e.content ?? "")),
    ).toBeDefined();
  });

  it("does NOT fire when the estimate is comfortably under 95%", async () => {
    // Keep the real 131k budget — a normal conversation won't trip.
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "be brief" }),
      stream: false,
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("hi")) {
      events.push({ role: ev.role, content: ev.content });
    }

    const anyPreflight = events.find(
      (e) => e.role === "warning" && /^preflight:/.test(e.content ?? ""),
    );
    expect(anyPreflight).toBeUndefined();
  });

  it("warns (but does not block) when over 95% with nothing to truncate", async () => {
    // Tiny budget AND a system prompt that alone overwhelms it. The log
    // is empty, so truncation has nothing to shrink — the preflight surfaces
    // a warning so the failure isn't mysterious; the request goes out
    // regardless and DeepSeek decides.
    DEEPSEEK_CONTEXT_TOKENS[TEST_MODEL] = 500;
    const bulkyPrompt = "You are a careful assistant. ".repeat(300);

    const client = makeClient([{ content: "ack" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: bulkyPrompt }),
      stream: false,
      model: TEST_MODEL,
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("hi")) {
      events.push({ role: ev.role, content: ev.content });
    }

    const warn = events.find((e) => e.role === "warning" && /^preflight:/.test(e.content ?? ""));
    expect(warn).toBeDefined();
    expect(warn!.content).toMatch(/nothing left to truncate/);
    // Run still reaches the final step — the user sees the warning
    // and can react, but we don't short-circuit on our own.
    const finals = events.filter((e) => e.role === "assistant_final");
    expect(finals.length).toBe(1);
  });
});
