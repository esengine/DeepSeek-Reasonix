import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { SKILL_PIN_MEMO_HEADER } from "../src/context-manager.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import type { ChatMessage } from "../src/types.js";

interface FakeResponseShape {
  content?: string;
}

interface CapturedRequest {
  messages: ChatMessage[];
}

function fakeFetch(responses: FakeResponseShape[], captured?: CapturedRequest[]): typeof fetch {
  let i = 0;
  return vi.fn(async (_url: unknown, init: { body?: string } | undefined) => {
    if (captured) {
      const body = init?.body ? JSON.parse(init.body) : {};
      captured.push({ messages: (body.messages ?? []) as ChatMessage[] });
    }
    const resp = responses[i++] ?? responses[responses.length - 1]!;
    return new Response(
      JSON.stringify({
        choices: [
          {
            index: 0,
            message: { role: "assistant", content: resp.content ?? "" },
            finish_reason: "stop",
          },
        ],
        usage: { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120 },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }) as unknown as typeof fetch;
}

function makeClient(responses: FakeResponseShape[], captured?: CapturedRequest[]) {
  return new DeepSeekClient({ apiKey: "sk-test", fetch: fakeFetch(responses, captured) });
}

function pin(name: string, body: string): string {
  return `<skill-pin name="${name}">\n${body}\n</skill-pin>`;
}

function seedTurns(loop: CacheFirstLoop, pairs: Array<{ user: string; assistant: string }>): void {
  for (const { user, assistant } of pairs) {
    loop.log.append({ role: "user", content: user });
    loop.log.append({ role: "assistant", content: assistant });
  }
}

describe("ContextManager fold preserves skill-pin bodies", () => {
  it("re-attaches a pinned skill body verbatim after summarization", async () => {
    const client = makeClient([{ content: "earlier turns discussed auth and billing." }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const skillBody = pin(
      "explore",
      "# Skill: explore\n\nStep 1. Read entrypoints.\nStep 2. Trace data flow.",
    );
    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c1", content: skillBody });
    seedTurns(loop, [
      { user: "q0 lots of bulk to weigh it", assistant: "a0 with similar bulk to weigh" },
      { user: "q1 lots of bulk to weigh it", assistant: "a1 with similar bulk to weigh" },
      { user: "q2 lots of bulk to weigh it", assistant: "a2 with similar bulk to weigh" },
      { user: "q3 lots of bulk to weigh it", assistant: "a3 with similar bulk to weigh" },
      { user: "q4 lots of bulk to weigh it", assistant: "a4 with similar bulk to weigh" },
    ]);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);

    const head = loop.log.entries[0]!;
    expect(head.role).toBe("assistant");
    const content = head.content as string;
    expect(content).toMatch(/HISTORY SUMMARY/);
    expect(content).toContain(SKILL_PIN_MEMO_HEADER);
    expect(content).toContain(skillBody);
  });

  it("dedupes repeated invocations of the same skill, keeping the most recent", async () => {
    const client = makeClient([{ content: "earlier turns happened." }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const first = pin("explore", "# Skill: explore\n\nFirst version of the body.");
    const second = pin("explore", "# Skill: explore\n\nSecond version of the body.");

    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c1", content: first });
    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c2", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c2", content: second });
    seedTurns(loop, [
      { user: "q0 padding text to ensure foldable", assistant: "a0 padding text to ensure" },
      { user: "q1 padding text to ensure foldable", assistant: "a1 padding text to ensure" },
      { user: "q2 padding text to ensure foldable", assistant: "a2 padding text to ensure" },
      { user: "q3 padding text to ensure foldable", assistant: "a3 padding text to ensure" },
    ]);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);

    const head = loop.log.entries[0]!;
    const content = head.content as string;
    expect(content).toContain("Second version of the body");
    expect(content).not.toContain("First version of the body");
  });

  it("does not break tool_call/tool pairing when stubbing pinned bodies", async () => {
    const client = makeClient([{ content: "summary text." }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({
      role: "tool",
      tool_call_id: "c1",
      content: pin("review", "# Skill: review\n\nReview checklist."),
    });
    seedTurns(loop, [
      { user: "q0 padding line for token weight", assistant: "a0 padding line for token" },
      { user: "q1 padding line for token weight", assistant: "a1 padding line for token" },
      { user: "q2 padding line for token weight", assistant: "a2 padding line for token" },
      { user: "q3 padding line for token weight", assistant: "a3 padding line for token" },
    ]);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);

    const entries = loop.log.entries;
    for (let i = 0; i < entries.length; i++) {
      const m = entries[i]!;
      if (m.role === "tool") {
        const prev = entries[i - 1];
        const prevHasMatchingCall =
          prev?.role === "assistant" &&
          Array.isArray(prev.tool_calls) &&
          prev.tool_calls.some((c) => c.id === m.tool_call_id);
        expect(prevHasMatchingCall).toBe(true);
      }
    }
  });

  it("folds normally when no skill-pin is present", async () => {
    const client = makeClient([{ content: "earlier turns covered routine work." }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    seedTurns(loop, [
      { user: "q0 enough bulk to fold for sure", assistant: "a0 enough bulk to fold sure" },
      { user: "q1 enough bulk to fold for sure", assistant: "a1 enough bulk to fold sure" },
      { user: "q2 enough bulk to fold for sure", assistant: "a2 enough bulk to fold sure" },
      { user: "q3 enough bulk to fold for sure", assistant: "a3 enough bulk to fold sure" },
      { user: "q4 enough bulk to fold for sure", assistant: "a4 enough bulk to fold sure" },
    ]);

    const result = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(result.folded).toBe(true);

    const head = loop.log.entries[0]!;
    const content = head.content as string;
    expect(content).toMatch(/HISTORY SUMMARY/);
    expect(content).not.toContain(SKILL_PIN_MEMO_HEADER);
  });

  it("re-pins bodies through a second fold (preserves through cascading folds)", async () => {
    const client = makeClient([
      { content: "first fold summary." },
      { content: "second fold summary." },
    ]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const skillBody = pin("explore", "# Skill: explore\n\nCarry me through every fold.");
    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c1", content: skillBody });
    seedTurns(loop, [
      { user: "q0 first round of weight", assistant: "a0 first round of weight" },
      { user: "q1 first round of weight", assistant: "a1 first round of weight" },
      { user: "q2 first round of weight", assistant: "a2 first round of weight" },
      { user: "q3 first round of weight", assistant: "a3 first round of weight" },
    ]);

    const r1 = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(r1.folded).toBe(true);
    expect(loop.log.entries[0]!.content as string).toContain(skillBody);

    seedTurns(loop, [
      { user: "q4 second round of weight", assistant: "a4 second round of weight" },
      { user: "q5 second round of weight", assistant: "a5 second round of weight" },
      { user: "q6 second round of weight", assistant: "a6 second round of weight" },
    ]);

    const r2 = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(r2.folded).toBe(true);
    expect(loop.log.entries[0]!.content as string).toContain(skillBody);
  });

  it("e2e: post-fold step sends the skill body verbatim to the model", async () => {
    const captured: CapturedRequest[] = [];
    const client = makeClient(
      [{ content: "earlier turns were summarized." }, { content: "got it, continuing." }],
      captured,
    );
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const skillBody = pin(
      "explore",
      "# Skill: explore\n\nFollow these steps strictly: 1) read entrypoints 2) trace flow 3) report.",
    );
    loop.log.append({
      role: "assistant",
      content: null,
      tool_calls: [
        { id: "c1", type: "function", function: { name: "run_skill", arguments: "{}" } },
      ],
    });
    loop.log.append({ role: "tool", tool_call_id: "c1", content: skillBody });
    seedTurns(loop, [
      { user: "q0 padding line for token weight", assistant: "a0 padding line for token" },
      { user: "q1 padding line for token weight", assistant: "a1 padding line for token" },
      { user: "q2 padding line for token weight", assistant: "a2 padding line for token" },
      { user: "q3 padding line for token weight", assistant: "a3 padding line for token" },
    ]);

    const r = await loop.compactHistory({ keepRecentTokens: 40 });
    expect(r.folded).toBe(true);
    captured.length = 0;

    const events: string[] = [];
    for await (const ev of loop.step("what's next?")) {
      events.push(ev.role);
    }
    expect(events).toContain("assistant_final");

    expect(captured.length).toBeGreaterThan(0);
    const stepRequest = captured[0]!;
    const serialized = JSON.stringify(stepRequest.messages);
    expect(serialized).toContain(SKILL_PIN_MEMO_HEADER);
    expect(serialized).toContain("Follow these steps strictly");
    expect(serialized).toContain("read entrypoints");
  });
});
