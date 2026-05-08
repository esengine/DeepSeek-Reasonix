/** CacheFirstLoop integration — fake-fetch DeepSeekClient, non-streaming path. */

import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { type ConfirmationChoice, PauseGate } from "../src/core/pause-gate.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import type { RepairReport } from "../src/repair/index.js";
import { ToolRegistry } from "../src/tools.js";
import type { ChatMessage } from "../src/types.js";

interface FakeResponseShape {
  content?: string;
  reasoning_content?: string;
  tool_calls?: any[];
  usage?: Record<string, number>;
}

function fakeFetch(responses: FakeResponseShape[]): typeof fetch {
  let i = 0;
  return vi.fn(async (_url: any, init: any) => {
    const body = init?.body ? JSON.parse(init.body) : {};
    const resp = responses[i++] ?? responses[responses.length - 1]!;
    return new Response(
      JSON.stringify({
        _echo_messages: body.messages as ChatMessage[],
        choices: [
          {
            index: 0,
            message: {
              role: "assistant",
              content: resp.content ?? "",
              reasoning_content: resp.reasoning_content ?? null,
              tool_calls: resp.tool_calls ?? undefined,
            },
            finish_reason: resp.tool_calls ? "tool_calls" : "stop",
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
}

function makeClient(responses: FakeResponseShape[]) {
  return new DeepSeekClient({
    apiKey: "sk-test",
    fetch: fakeFetch(responses),
  });
}

describe("CacheFirstLoop (non-streaming)", () => {
  it("completes a single-turn plain chat", async () => {
    const client = makeClient([{ content: "hi there" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "be brief" }),
      stream: false,
    });

    const events: string[] = [];
    for await (const ev of loop.step("hello")) {
      events.push(ev.role);
    }

    expect(events).toContain("assistant_final");
    expect(events[events.length - 1]).toBe("done");
    expect(loop.stats.turns.length).toBe(1);
    expect(loop.log.length).toBe(2); // user + assistant
  });

  it("records cache hit telemetry from API usage", async () => {
    const client = makeClient([
      {
        content: "ok",
        usage: {
          prompt_tokens: 1000,
          completion_tokens: 10,
          total_tokens: 1010,
          prompt_cache_hit_tokens: 800,
          prompt_cache_miss_tokens: 200,
        },
      },
    ]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    await loop.run("q");
    expect(loop.stats.aggregateCacheHitRatio).toBeCloseTo(0.8);
    expect(loop.stats.totalCost).toBeGreaterThan(0);
    // Savings vs Claude depends on which DeepSeek model is the loop's
    // default. v4-pro lands around 0.85; v4-flash around 0.97. Test the
    // lower bound so a future default swap doesn't churn this assertion.
    expect(loop.stats.savingsVsClaude).toBeGreaterThan(0.8);
  });

  it("dispatches a tool call and loops until the model stops", async () => {
    const client = makeClient([
      {
        content: "",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: { name: "add", arguments: '{"a":2,"b":3}' },
          },
        ],
      },
      { content: "The answer is 5." },
    ]);

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

    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({
        system: "use add tool",
        toolSpecs: tools.specs(),
      }),
      tools,
      stream: false,
    });

    const roles: string[] = [];
    let toolContent = "";
    let finalContent = "";
    for await (const ev of loop.step("2 + 3 = ?")) {
      roles.push(ev.role);
      if (ev.role === "tool") toolContent = ev.content;
      if (ev.role === "assistant_final") finalContent = ev.content;
    }

    expect(roles).toContain("tool");
    expect(toolContent).toBe("5");
    expect(finalContent).toBe("The answer is 5.");
    expect(loop.stats.turns.length).toBe(2); // two model round-trips
  });

  it("yields tool_start before each tool dispatch so the TUI can show 'running…'", async () => {
    const client = makeClient([
      {
        content: "",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: { name: "add", arguments: '{"a":1,"b":2}' },
          },
        ],
      },
      { content: "done" },
    ]);
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
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    const roleOrder: { role: string; toolName?: string }[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool_start" || ev.role === "tool") {
        roleOrder.push({ role: ev.role, toolName: ev.toolName });
      }
    }
    // tool_start must precede the matching tool result.
    expect(roleOrder[0]).toEqual({ role: "tool_start", toolName: "add" });
    expect(roleOrder[1]).toEqual({ role: "tool", toolName: "add" });
  });

  it("immutable prefix is preserved across turns (cache-stability invariant)", async () => {
    const sharedFetch = fakeFetch([{ content: "a" }, { content: "b" }]);
    const client = new DeepSeekClient({ apiKey: "sk-test", fetch: sharedFetch });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "pinned system" }),
      stream: false,
    });

    await loop.run("q1");
    await loop.run("q2");

    const calls = (sharedFetch as any).mock.calls;
    expect(calls.length).toBe(2);
    const msgs1 = JSON.parse(calls[0][1].body).messages as ChatMessage[];
    const msgs2 = JSON.parse(calls[1][1].body).messages as ChatMessage[];

    // Both requests start with the exact same system prefix (byte-identical).
    expect(msgs1[0]).toEqual({ role: "system", content: "pinned system" });
    expect(msgs2[0]).toEqual({ role: "system", content: "pinned system" });

    // Second request should begin with msgs1 as its prefix
    // (append-only log invariant: history is never rewritten).
    for (let i = 0; i < msgs1.length; i++) {
      expect(msgs2[i]).toEqual(msgs1[i]);
    }
    // And msgs2 is strictly longer (new user turn + assistant reply from turn 1).
    expect(msgs2.length).toBeGreaterThan(msgs1.length);
  });

  it("yields a warning event once when tool-call count crosses 70% of budget", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const callAgain = {
      content: "",
      tool_calls: [{ id: "c", type: "function", function: { name: "probe", arguments: "{}" } }],
    };
    const summary = { content: "all done" };
    const responses: FakeResponseShape[] = [callAgain, callAgain, callAgain, callAgain, summary];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 4, // 70% → warn starting at iter >= 2
    });

    const warnings: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "warning") warnings.push(ev.content);
    }
    // Identical fixture calls also trip the storm breaker in 0.4.19+,
    // which emits its own warning. Filter for the iter-budget warning
    // specifically — that's what this test guards (once-per-turn flag).
    const iterBudgetWarnings = warnings.filter((w) => /tool calls used/.test(w));
    expect(iterBudgetWarnings).toHaveLength(1);
    expect(iterBudgetWarnings[0]).toMatch(/\d+\/4 tool calls used/);
    expect(iterBudgetWarnings[0]).toMatch(/Esc/);
  });

  it("abort() mid-step stops immediately without a follow-up API call", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const chainingToolCall = {
      content: "",
      tool_calls: [{ id: "c", type: "function", function: { name: "probe", arguments: "{}" } }],
    };
    // Only one chaining response needed — abort should stop the loop
    // before any follow-up model call. A second response in the array
    // would indicate the loop made an unwanted extra API call.
    const fetchSpy = vi.fn() as unknown as typeof fetch;
    const responses: FakeResponseShape[] = [chainingToolCall];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(responses) as unknown as typeof fetch,
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 16,
    });

    // Call abort AFTER the first tool event fires — simulates the user
    // hitting Esc while the loop is exploring.
    const events: { role: string; content?: string; forcedSummary?: boolean }[] = [];
    let aborted = false;
    for await (const ev of loop.step("go")) {
      events.push({ role: ev.role, content: ev.content, forcedSummary: ev.forcedSummary });
      if (!aborted && ev.role === "tool") {
        aborted = true;
        loop.abort();
      }
    }

    // Warning fires with the abort notice.
    const warnings = events.filter((e) => e.role === "warning");
    expect(warnings.some((w) => /aborted at iter/.test(w.content ?? ""))).toBe(true);

    // Synthetic assistant_final is tagged forcedSummary and carries
    // the stopped-message text. It should NOT contain any model
    // output because no second API call was made.
    const finals = events.filter((e) => e.role === "assistant_final");
    const stopped = finals[finals.length - 1]!;
    expect(stopped.forcedSummary).toBe(true);
    expect(stopped.content).toMatch(/aborted by user \(Esc\)/);
    expect(stopped.content).toMatch(/no summary produced/);

    // Suite ends with `done`.
    expect(events[events.length - 1]!.role).toBe("done");
    // Silence unused-var warning.
    void fetchSpy;
  });

  it("does not bleed the prior turn's abort into the next step", async () => {
    // Regression: a user pressing Esc once would put _turnAbort into
    // an aborted state; the iter-0 abort branch handled it but didn't
    // reset the controller. Every subsequent step() then carried the
    // stale aborted state forward and bailed out with another
    // "stopped without producing a summary" before any model call ran.
    // The session was effectively dead until restart.
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const chainingToolCall = {
      content: "",
      tool_calls: [{ id: "c", type: "function", function: { name: "probe", arguments: "{}" } }],
    };
    const finalAnswer = { content: "second turn ran cleanly", tool_calls: [] };
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch([chainingToolCall, finalAnswer]) as unknown as typeof fetch,
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 16,
    });

    // Turn 1 — abort mid-flight.
    let aborted = false;
    for await (const ev of loop.step("first")) {
      if (!aborted && ev.role === "tool") {
        aborted = true;
        loop.abort();
      }
    }

    // Turn 2 — fresh user input; should reach the second model call
    // and yield its output. If the bug is back, we see iter-0 abort
    // again and never see "second turn ran cleanly".
    const turn2Events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("second")) {
      turn2Events.push({ role: ev.role, content: ev.content });
    }

    const finals = turn2Events.filter((e) => e.role === "assistant_final");
    expect(finals).toHaveLength(1);
    expect(finals[0]!.content).toBe("second turn ran cleanly");
    // No "aborted at iter 0" warning on turn 2.
    expect(
      turn2Events.some((e) => e.role === "warning" && /aborted at iter/.test(e.content ?? "")),
    ).toBe(false);
  });

  it("forces a summary when maxToolIters is exhausted, instead of stopping silently", async () => {
    // Give a registered tool so the repair layer doesn't strip the fake
    // tool_calls for referring to an unknown name.
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    // Every tool-iter response says "call probe again" — infinite loop
    // absent the iter cap. The (N+1)th response is the forced-summary
    // call (no tools, returns text).
    const chainingToolCall = {
      content: "",
      tool_calls: [
        {
          id: "call_1",
          type: "function",
          function: { name: "probe", arguments: "{}" },
        },
      ],
    };
    const responses: FakeResponseShape[] = [
      chainingToolCall,
      chainingToolCall,
      { content: "done — here's what I found." }, // summary call
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 2, // deliberately tight so we hit the cap fast
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("go")) {
      events.push({ role: ev.role, content: ev.content });
    }

    // Multiple assistant_final events are yielded (one per iter) — the
    // summary is the LAST one, carrying the "tool-call budget" prefix.
    const finals = events.filter((e) => e.role === "assistant_final");
    const summary = finals[finals.length - 1];
    expect(summary).toBeDefined();
    expect(summary!.content).toMatch(/tool-call budget/);
    expect(summary!.content).toContain("done — here's what I found.");
    // Last event is still `done`, preserving the contract used by run().
    expect(events[events.length - 1]!.role).toBe("done");
  });

  it("first all-suppressed storm self-corrects in-turn instead of stopping", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const dupCall = {
      id: "c1",
      type: "function",
      function: { name: "probe", arguments: "{}" },
    };
    const responses: FakeResponseShape[] = [
      { content: "", tool_calls: [dupCall] },
      { content: "", tool_calls: [{ ...dupCall, id: "c2" }] },
      { content: "", tool_calls: [{ ...dupCall, id: "c3" }] },
      { content: "got it — done." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 8,
    });

    const events: { role: string; forcedSummary?: boolean; content?: string }[] = [];
    for await (const ev of loop.step("explore")) {
      events.push({ role: ev.role, forcedSummary: ev.forcedSummary, content: ev.content });
    }

    expect(
      events.some((e) => e.role === "warning" && /repeated tool call/i.test(e.content ?? "")),
    ).toBe(true);
    expect(
      events.some((e) => e.role === "warning" && /stuck retry loop/i.test(e.content ?? "")),
    ).toBe(false);

    const finals = events.filter((e) => e.role === "assistant_final");
    const final = finals[finals.length - 1];
    expect(final?.forcedSummary).toBeFalsy();
    expect(final?.content).toBe("got it — done.");

    const tail = loop.log.entries[loop.log.entries.length - 1];
    expect(tail?.role).toBe("assistant");
  });

  it("second all-suppressed storm in same turn falls back to forced summary", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const dupCall = {
      id: "c1",
      type: "function",
      function: { name: "probe", arguments: "{}" },
    };
    const responses: FakeResponseShape[] = [
      { content: "", tool_calls: [dupCall] },
      { content: "", tool_calls: [{ ...dupCall, id: "c2" }] },
      { content: "", tool_calls: [{ ...dupCall, id: "c3" }] },
      { content: "", tool_calls: [{ ...dupCall, id: "c4" }] },
      { content: "", tool_calls: [{ ...dupCall, id: "c5" }] },
      { content: "i would summarize but the harness should override me." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 8,
    });

    const events: { role: string; forcedSummary?: boolean; content?: string }[] = [];
    for await (const ev of loop.step("explore")) {
      events.push({ role: ev.role, forcedSummary: ev.forcedSummary, content: ev.content });
    }

    expect(
      events.some((e) => e.role === "warning" && /stuck retry loop/i.test(e.content ?? "")),
    ).toBe(true);

    const finals = events.filter((e) => e.role === "assistant_final");
    const summary = finals[finals.length - 1];
    expect(summary?.forcedSummary).toBe(true);
    expect(summary?.content).toMatch(/stuck on a repeated tool call/);
  });

  it("context-guard diverts to summary when promptTokens > 80% of the window, tagging forcedSummary", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    // First response: chaining tool call with a prompt-token count
    // deliberately over 80% of DeepSeek V4's 1M window (1M * 0.8 =
    // 800k). 900k trips the guard.
    const responses: FakeResponseShape[] = [
      {
        content: "",
        tool_calls: [{ id: "c", type: "function", function: { name: "probe", arguments: "{}" } }],
        usage: {
          prompt_tokens: 900_000,
          completion_tokens: 50,
          total_tokens: 900_050,
          prompt_cache_hit_tokens: 700_000,
          prompt_cache_miss_tokens: 200_000,
        },
      },
      // Forced-summary response (no tools)
      { content: "based on what I saw, X." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 64,
    });

    const events: { role: string; forcedSummary?: boolean; content?: string }[] = [];
    for await (const ev of loop.step("analyze the repo")) {
      events.push({ role: ev.role, forcedSummary: ev.forcedSummary, content: ev.content });
    }

    // A warning must fire about the context guard. Accept both the
    // auto-compact-saved-us variant and the nothing-to-compact variant
    // — the message format shifted in 0.4.11 when we added the
    // auto-compact attempt before forcing summary.
    const warn = events.find((e) => e.role === "warning");
    expect(warn).toBeDefined();
    expect(warn!.content).toMatch(/context [\d,]+\/[\d,]+/);

    // The final assistant_final must be tagged forcedSummary and carry the context-guard prefix.
    const finals = events.filter((e) => e.role === "assistant_final");
    const summary = finals[finals.length - 1];
    expect(summary!.forcedSummary).toBe(true);
    expect(summary!.content).toMatch(/context budget running low/);
  });

  it("compactHistory replaces head with summary, keeps tail within token budget", async () => {
    const responses: FakeResponseShape[] = [
      { content: "User explored auth and billing modules; landed on session refactor plan." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    // Seed 6 user/assistant pairs with chunky content so we can
    // reason about token weight; each pair ≈ 20 tokens.
    for (let i = 0; i < 6; i++) {
      loop.log.append({
        role: "user",
        content: `question number ${i} with some words to weigh it`,
      });
      loop.log.append({ role: "assistant", content: `answer number ${i} with similar bulk` });
    }
    expect(loop.log.length).toBe(12);

    // Budget of ~60 tokens fits ~3 trailing pairs.
    const result = await loop.compactHistory({ keepRecentTokens: 60 });
    expect(result.folded).toBe(true);
    expect(result.beforeMessages).toBe(12);
    expect(result.afterMessages).toBeLessThan(12);

    const entries = loop.log.entries;
    expect(entries[0]!.role).toBe("assistant");
    expect(entries[0]!.content as string).toMatch(/HISTORY SUMMARY/);
    expect(entries[1]!.role).toBe("user");
    expect(entries[entries.length - 1]!.content).toMatch(/answer number 5/);
  });

  it("compactHistory no-ops when head wouldn't shrink log meaningfully", async () => {
    const client = makeClient([{ content: "summary" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "user", content: "q0" });
    loop.log.append({ role: "assistant", content: "a0" });
    loop.log.append({ role: "user", content: "q1" });
    loop.log.append({ role: "assistant", content: "a1" });

    // Budget large enough to cover everything → no fold needed.
    const result = await loop.compactHistory({ keepRecentTokens: 10_000 });
    expect(result.folded).toBe(false);
    expect(loop.log.length).toBe(4);
  });

  it("auto-folds history when promptTokens crosses 50% of ctxMax", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const responses: FakeResponseShape[] = [
      // Iter 0: tool call with usage above 50% of 1M ctx.
      {
        content: "",
        tool_calls: [{ id: "c1", type: "function", function: { name: "probe", arguments: "{}" } }],
        usage: {
          prompt_tokens: 600_000,
          completion_tokens: 10,
          total_tokens: 600_010,
          prompt_cache_hit_tokens: 500_000,
          prompt_cache_miss_tokens: 100_000,
        },
      },
      // Summary call response (compactHistory).
      { content: "Earlier turns explored topic X and decided Y." },
      // Iter 1 (after fold): wrap-up.
      { content: "done." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 8,
    });
    // Seed 18 user/assistant turns sized so the LOG estimate stays
    // below the 95% preflight threshold (otherwise preflight folds
    // first and the auto-fold path never runs). The mocked usage of
    // 600k below is what trips the auto-fold check, independent of the
    // tokenizer's view of the seed.
    const fillLines = (label: string, n: number) =>
      Array.from(
        { length: n },
        (_, i) =>
          `${label} line ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`,
      ).join("\n");
    for (let i = 0; i < 18; i++) {
      loop.log.append({ role: "user", content: `Q${i}\n${fillLines(`q${i}`, 400)}` });
      loop.log.append({ role: "assistant", content: `A${i}\n${fillLines(`a${i}`, 400)}` });
    }
    const beforeMessages = loop.log.length;

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("continue")) {
      events.push({ role: ev.role, content: ev.content });
    }

    const foldWarn = events.find(
      (e) => e.role === "warning" && /folded \d+ messages/.test(e.content ?? ""),
    );
    expect(foldWarn).toBeDefined();
    expect(foldWarn!.content).toMatch(/folded \d+ messages/);
    expect(loop.log.length).toBeLessThan(beforeMessages);
  }, 30_000);

  it("uses the aggressive fold tier when promptTokens crosses 70% of ctxMax", async () => {
    const reg = new ToolRegistry();
    reg.register({
      name: "probe",
      description: "no-op",
      parameters: { type: "object", properties: {} },
      fn: async () => "ok",
    });
    const responses: FakeResponseShape[] = [
      // Iter 0: usage at 75% of 1M ctx — squarely in the aggressive band.
      {
        content: "",
        tool_calls: [{ id: "c1", type: "function", function: { name: "probe", arguments: "{}" } }],
        usage: {
          prompt_tokens: 750_000,
          completion_tokens: 10,
          total_tokens: 750_010,
          prompt_cache_hit_tokens: 600_000,
          prompt_cache_miss_tokens: 150_000,
        },
      },
      // Summary call (compactHistory).
      { content: "Earlier turns covered topic X." },
      // Iter 1 wrap-up.
      { content: "done." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      maxToolIters: 8,
    });
    const fillLines = (label: string, n: number) =>
      Array.from(
        { length: n },
        (_, i) =>
          `${label} line ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`,
      ).join("\n");
    for (let i = 0; i < 18; i++) {
      loop.log.append({ role: "user", content: `Q${i}\n${fillLines(`q${i}`, 400)}` });
      loop.log.append({ role: "assistant", content: `A${i}\n${fillLines(`a${i}`, 400)}` });
    }

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("continue")) {
      events.push({ role: ev.role, content: ev.content });
    }

    // The warning should call out the aggressive tier explicitly.
    const foldWarn = events.find(
      (e) => e.role === "warning" && /aggressively folded/.test(e.content ?? ""),
    );
    expect(foldWarn).toBeDefined();
    // And the status line should advertise it too, so users know why
    // recent context got trimmed harder than usual.
    const status = events.find((e) => e.role === "status" && /aggressive/.test(e.content ?? ""));
    expect(status).toBeDefined();
  }, 30_000);

  it("pre-clips new tool results at dispatch so they never enter the log oversized", async () => {
    const reg = new ToolRegistry();
    // Tool returns ~50k chars of realistic-shape log text; the default
    // token budget (8k) bounds the resulting log entry to a small
    // fraction of the raw size. (Using "A".repeat(N) would hit the
    // tokenizer's BPE O(n²) path for repeated single-char inputs —
    // pathological enough to slow the suite by tens of seconds, and
    // not representative of real tool output.)
    const huge = "ERROR: repeated failure with some detail\n".repeat(1250);
    reg.register({
      name: "big",
      description: "returns a lot",
      parameters: { type: "object", properties: {} },
      fn: async () => huge,
    });
    const responses: FakeResponseShape[] = [
      {
        content: "",
        tool_calls: [{ id: "c1", type: "function", function: { name: "big", arguments: "{}" } }],
      },
      { content: "summarized." },
    ];
    const client = makeClient(responses);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
    });
    for await (const _ev of loop.step("go")) {
      /* drain */
    }
    const toolEntry = loop.log.toMessages().find((m) => m.role === "tool");
    expect(toolEntry).toBeDefined();
    const content = typeof toolEntry!.content === "string" ? toolEntry!.content : "";
    // Well under the raw 50k — pre-clip fired before append.
    expect(content.length).toBeLessThan(40_000);
    expect(content).toMatch(/truncated/);
  });

  it("buildMessages strips a dangling assistant-with-tool_calls tail — defensive against 'insufficient tool messages' 400", async () => {
    // Craft a log where the last entry is an assistant message with
    // tool_calls but no matching tool responses. This is the shape
    // that used to crash the forced-summary call with DeepSeek's
    // 'insufficient tool messages following tool_calls' error.
    const client = makeClient([{ content: "summary text" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "user", content: "hi" });
    loop.log.append({
      role: "assistant",
      content: "",
      tool_calls: [{ id: "x", type: "function", function: { name: "noop", arguments: "{}" } }],
    });
    // A chat turn from here should succeed, not 400, because
    // buildMessages strips the unpaired tail.
    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("continue")) {
      events.push({ role: ev.role, content: ev.content });
    }
    expect(events.find((e) => e.role === "error")).toBeUndefined();
    // The fake fetch echoes the messages it received — no unpaired
    // assistant+tool_calls should be in there.
    expect(events.find((e) => e.role === "assistant_final")?.content).toContain("summary text");
  });

  it("surfaces an error event when the HTTP call fails with a non-retryable status", async () => {
    // 401 is non-retryable (bad key). Using this avoids multi-retry waits.
    const errFetch = vi.fn(async () => new Response("boom", { status: 401 }));
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: errFetch as unknown as typeof fetch,
      retry: { initialBackoffMs: 1, maxAttempts: 1 },
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const roles: string[] = [];
    for await (const ev of loop.step("q")) {
      roles.push(ev.role);
    }
    expect(roles).toContain("error");
  });
});

// ── Test helper: call the private noteToolFailureSignal method ──────────
// PRIVATE-ACCESS JUSTIFICATION: noteToolFailureSignal is private, and the
// counter state lives inside the private TurnFailureTracker (_turnFailures)
// — there is no public getter for the current count / type breakdown, and
// `escalatedThisTurn` only reflects the boolean outcome, not the tally
// that produced it. The SEARCH-mismatch path is tested behaviorally through
// step() below (driving real tool failures and asserting on escalatedThisTurn
// + warning events). The repair-based path (scavenged/truncationsFixed/
// stormsBroken) is also reachable through step() — step() calls
// noteToolFailureSignal("", report) internally — but constructing specific
// RepairReport inputs requires tool-call patterns that are deeply coupled
// to repair-module internals (scavenge scanners, storm-threshold windows,
// truncation JSON shapes). Testing the counting + threshold logic directly
// with known inputs keeps these tests focused on the escalation gate rather
// than the repair pipeline that feeds it. All private-field access is
// consolidated behind this single helper so only one place needs updating
// when the representation changes.
function signalToolFailure(
  loop: CacheFirstLoop,
  options: {
    /** Set the accumulated failure count before this call (default 0). */
    count?: number;
    /** Set the already-escalated flag before this call (default false). */
    escalated?: boolean;
    /** Disable autoEscalate for this loop (reconfigures the instance). */
    disableAutoEscalate?: boolean;
    /** A tool-result JSON string to scan for SEARCH-mismatch patterns. */
    resultJson?: string;
    /** A repair report whose counts contribute to the failure tally. */
    repair?: RepairReport;
  } = {},
): { escalated: boolean; count: number; types: Record<string, number> } {
  if (options.disableAutoEscalate) loop.configure({ autoEscalate: false });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const priv = loop as any;
  const tracker = priv._turnFailures as { count: number; types: Record<string, number> };
  if (options.count !== undefined) tracker.count = options.count;
  if (options.escalated !== undefined) priv._escalateThisTurn = options.escalated;

  const escalated: boolean = priv.noteToolFailureSignal(options.resultJson ?? "", options.repair);
  return {
    escalated,
    count: tracker.count as number,
    types: { ...tracker.types } as Record<string, number>,
  };
}

describe("CacheFirstLoop - auto-escalation on tool failures", () => {
  const FAILURE_ESCALATION_THRESHOLD = 3;

  // ── Behavioral tests: drive real tool failures through step() ──────

  it("auto-escalates when 3 SEARCH-mismatch tool failures accumulate", async () => {
    const tools = new ToolRegistry();
    tools.register({
      name: "fail_tool",
      description: "returns SEARCH-mismatch error shape",
      parameters: { type: "object", properties: { n: { type: "integer" } }, required: [] },
      fn: ({ n }: { n?: number }) =>
        JSON.stringify({
          error: `Error: search text not found in file_${n ?? 0}.ts`,
        }),
    });
    // 3 tool calls, each with different args so the storm breaker
    // sees distinct signatures and doesn't suppress any.
    const call = (id: string, n: number) => ({
      content: "",
      tool_calls: [
        {
          id,
          type: "function" as const,
          function: { name: "fail_tool", arguments: JSON.stringify({ n }) },
        },
      ],
    });
    const client = makeClient([
      call("c1", 1),
      call("c2", 2),
      call("c3", 3),
      { content: "done after failures" },
    ]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    const warnings: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "warning") warnings.push(ev.content);
    }

    expect(loop.escalatedThisTurn).toBe(true);
    expect(warnings.some((w) => /escalat/i.test(w))).toBe(true);
  });

  it("single SEARCH-mismatch below threshold does not auto-escalate", async () => {
    const tools = new ToolRegistry();
    tools.register({
      name: "fail_tool",
      description: "returns SEARCH-mismatch error shape",
      parameters: { type: "object", properties: {}, required: [] },
      fn: () => JSON.stringify({ error: "Error: search text not found in file.ts" }),
    });
    const client = makeClient([
      {
        content: "",
        tool_calls: [
          { id: "c1", type: "function", function: { name: "fail_tool", arguments: "{}" } },
        ],
      },
      { content: "ok after one failure" },
    ]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    const warnings: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "warning") warnings.push(ev.content);
    }

    expect(loop.escalatedThisTurn).toBe(false);
    expect(warnings.some((w) => /escalat/i.test(w))).toBe(false);
  });

  it("autoEscalate=false prevents auto-escalation despite accumulated failures", async () => {
    const tools = new ToolRegistry();
    tools.register({
      name: "fail_tool",
      description: "returns SEARCH-mismatch error shape",
      parameters: { type: "object", properties: { n: { type: "integer" } }, required: [] },
      fn: ({ n }: { n?: number }) =>
        JSON.stringify({
          error: `Error: search text not found in file_${n ?? 0}.ts`,
        }),
    });
    const call = (id: string, n: number) => ({
      content: "",
      tool_calls: [
        {
          id,
          type: "function" as const,
          function: { name: "fail_tool", arguments: JSON.stringify({ n }) },
        },
      ],
    });
    const client = makeClient([call("c1", 1), call("c2", 2), call("c3", 3), { content: "done" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      autoEscalate: false,
    });

    const warnings: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "warning") warnings.push(ev.content);
    }

    expect(loop.escalatedThisTurn).toBe(false);
    expect(warnings.some((w) => /escalat/i.test(w))).toBe(false);
  });

  // ── Unit tests: edge cases that need private state access ─────────

  it("repair flavor counts accumulate proportionally per call", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    const result = signalToolFailure(loop, {
      repair: { scavenged: 2, truncationsFixed: 3, stormsBroken: 1, notes: [] },
    });
    // 2 (scavenged) + 3 (truncationsFixed) + 1 (stormsBroken) = 6
    expect(result.count).toBe(6);
    expect(result.types.scavenged).toBe(2);
    expect(result.types.truncated).toBe(3);
    expect(result.types["repeat-loop"]).toBe(1);
  });

  it("SEARCH-mismatch error result bumps the search-mismatch flavor", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    const result = signalToolFailure(loop, {
      resultJson: JSON.stringify({
        error: "Error: search text not found in path/to/file.ts",
      }),
    });
    expect(result.count).toBe(1);
    expect(result.types["search-mismatch"]).toBe(1);
  });

  it("non-SEARCH error results do not bump the counter", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    const result = signalToolFailure(loop, {
      count: 2,
      resultJson: JSON.stringify({ error: "some other error" }),
    });
    // Neither bumped (the error string lacks "search text not found")
    // nor escalated — the count stays at the preset value.
    expect(result.count).toBe(2);
    expect(result.escalated).toBe(false);
  });

  it("returns true and flips escalatedThisTurn when threshold is crossed", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    // One below threshold → should tip.
    const result = signalToolFailure(loop, {
      count: 2,
      repair: { scavenged: 0, truncationsFixed: 1, stormsBroken: 0, notes: [] },
    });
    expect(result.escalated).toBe(true);
    expect(result.count).toBe(3);
    // Public getter also reflects the escalation.
    expect(loop.escalatedThisTurn).toBe(true);
  });

  it("does not double-escalate when already escalated this turn", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    // Start one below threshold so the call WOULD cross and trigger
    // escalation, but the already-escalated flag must block it.
    const result = signalToolFailure(loop, {
      count: 2,
      escalated: true,
      repair: { scavenged: 1, truncationsFixed: 0, stormsBroken: 0, notes: [] },
    });
    expect(result.escalated).toBe(false); // no double-escalation
  });

  it("does not escalate when autoEscalate is disabled", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    // Start one below threshold so the call WOULD cross and trigger
    // escalation, but autoEscalate=false must block it.
    const result = signalToolFailure(loop, {
      count: 2,
      disableAutoEscalate: true,
      repair: { scavenged: 1, truncationsFixed: 0, stormsBroken: 0, notes: [] },
    });
    expect(result.escalated).toBe(false);
    expect(loop.escalatedThisTurn).toBe(false);
  });
});

describe("CacheFirstLoop - retryLastUser edge cases", () => {
  it("returns null when the only entry is not a user message", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "assistant", content: "answer" });
    const result = loop.retryLastUser();
    expect(result).toBeNull();
    // Log should be unchanged.
    expect(loop.log.length).toBe(1);
  });

  it("returns empty string when the last user message content is not a string", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    // Append a user message with array content (not a string).
    loop.log.append({ role: "user", content: ["not a string"] } as any);
    const result = loop.retryLastUser();
    // typeof raw === "string" → false, so userText = ""
    expect(result).toBe("");
    expect(loop.log.length).toBe(0); // messages after and including user were removed
  });

  it("preserves only messages before the LAST user, ignoring earlier users", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "user", content: "q1" });
    loop.log.append({ role: "assistant", content: "a1" });
    loop.log.append({ role: "user", content: "q2" });
    loop.log.append({ role: "assistant", content: "a2" });
    loop.log.append({ role: "user", content: "q3" });
    loop.log.append({ role: "assistant", content: "a3" });

    const result = loop.retryLastUser();
    expect(result).toBe("q3");
    // Messages up to q2/a2 should be preserved (4 entries), q3 and a3 removed.
    expect(loop.log.length).toBe(4);
    expect(loop.log.entries[0]!.content).toBe("q1");
    expect(loop.log.entries[3]!.content).toBe("a2");
  });

  it("returns null from empty log even with session name set", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    const result = loop.retryLastUser();
    expect(result).toBeNull();
  });

  it("returns content with complex value but asynchronously stores to session", async () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "user", content: "retry me" });
    loop.log.append({ role: "assistant", content: "answer" });

    const result = loop.retryLastUser();
    expect(result).toBe("retry me");
    // verify log was truncated to only messages before retry target
    expect(loop.log.length).toBe(0);
  });
});

describe("CacheFirstLoop - configure() method", () => {
  it("updates model via configure", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    expect(loop.model).toBe("deepseek-v4-flash");
    loop.configure({ model: "deepseek-v4-pro" });
    expect(loop.model).toBe("deepseek-v4-pro");
  });

  it("updates stream preference via configure", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: true,
    });
    expect(loop.stream).toBe(true);
    loop.configure({ stream: false });
    expect(loop._streamPreference).toBe(false);
    expect(loop.stream).toBe(false);
  });

  it("updates reasoningEffort via configure", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      reasoningEffort: "max",
    });
    loop.configure({ reasoningEffort: "high" });
    expect(loop.reasoningEffort).toBe("high");
  });

  it("updates autoEscalate via configure", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      autoEscalate: true,
    });
    loop.configure({ autoEscalate: false });
    expect(loop.autoEscalate).toBe(false);
  });
});

describe("CacheFirstLoop - setBudget / clearLog / retryLastUser / proArm", () => {
  it("setBudget(null) clears budget", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.setBudget(null);
    expect(loop.budgetUsd).toBeNull();
    // Re-arm of the 80%-warning latch is tested behaviorally in
    // "setBudget re-arms the 80% warning when the cap moves" below.
  });

  it("setBudget(0) clears budget to null (same as null)", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.setBudget(0);
    expect(loop.budgetUsd).toBeNull();
  });

  it("setBudget(positive) sets budget", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.setBudget(2.5);
    expect(loop.budgetUsd).toBe(2.5);
    // Re-arm of the 80%-warning latch is tested behaviorally in
    // "setBudget re-arms the 80% warning when the cap moves" below.
  });

  it("clearLog empties messages and resets scratch", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    // Seed log entries and scratch state.
    loop.log.append({ role: "user", content: "hello" });
    loop.log.append({ role: "assistant", content: "hi" });
    expect(loop.log.length).toBeGreaterThan(0);
    loop.scratch.notes = ["stale note"];
    loop.scratch.reasoning = "stale reasoning";

    const { dropped } = loop.clearLog();
    expect(dropped).toBe(2);
    expect(loop.log.length).toBe(0);
    expect(loop.scratch.notes).toEqual([]);
    expect(loop.scratch.reasoning).toBeNull();
  });

  it("clearLog returns 0 dropped when already empty", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    expect(loop.log.length).toBe(0);
    const { dropped } = loop.clearLog();
    expect(dropped).toBe(0);
  });

  it("retryLastUser returns null when no user message exists", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    const result = loop.retryLastUser();
    expect(result).toBeNull();
  });

  it("retryLastUser returns user text and removes messages after it", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.log.append({ role: "user", content: "my question" });
    loop.log.append({ role: "assistant", content: "an answer" });
    loop.log.append({ role: "user", content: "follow up" });
    loop.log.append({ role: "assistant", content: "follow-up answer" });

    const result = loop.retryLastUser();
    expect(result).toBe("follow up");
    // Messages after the last user (including it) should be removed.
    expect(loop.log.length).toBe(2);
    expect(loop.log.entries[0]!.content).toBe("my question");
    expect(loop.log.entries[1]!.content).toBe("an answer");
  });

  it("armProForNextTurn sets proArmed and step consumes it producing warning", async () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    expect(loop.proArmed).toBe(false);
    loop.armProForNextTurn();
    expect(loop.proArmed).toBe(true);

    // After step(), the arm is consumed.
    const warnings: string[] = [];
    for await (const ev of loop.step("hi")) {
      if (ev.role === "warning") warnings.push(ev.content);
    }
    // Should have a warning about /pro armed.
    expect(warnings.some((w) => /\/pro armed/.test(w))).toBe(true);
    expect(loop.proArmed).toBe(false);
    // escalatedThisTurn should be true because the arm was consumed.
    expect(loop.escalatedThisTurn).toBe(true);
  });

  it("disarmPro cancels arming before step", () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    loop.armProForNextTurn();
    expect(loop.proArmed).toBe(true);
    loop.disarmPro();
    expect(loop.proArmed).toBe(false);
  });

  it("escalatedThisTurn is false when not armed and no auto-escalation triggered", async () => {
    const client = makeClient([{ content: "ok" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
      autoEscalate: false,
    });
    expect(loop.escalatedThisTurn).toBe(false);
    // Run a step - no escalation should occur.
    for await (const _ev of loop.step("hi")) {
      /* drain */
    }
    expect(loop.escalatedThisTurn).toBe(false);
  });
});

describe("CacheFirstLoop - self-reported escalation via <<<NEEDS_PRO>>>", () => {
  function modelCapturingFetch(responses: FakeResponseShape[]): {
    fetch: typeof fetch;
    seenModels: string[];
  } {
    const seenModels: string[] = [];
    let i = 0;
    const fetchFn = vi.fn(async (_url: any, init: any) => {
      const body = init?.body ? JSON.parse(init.body) : {};
      seenModels.push(body.model);
      const resp = responses[i++] ?? responses[responses.length - 1]!;
      return new Response(
        JSON.stringify({
          choices: [
            {
              index: 0,
              message: {
                role: "assistant",
                content: resp.content ?? "",
                reasoning_content: resp.reasoning_content ?? null,
                tool_calls: resp.tool_calls ?? undefined,
              },
              finish_reason: resp.tool_calls ? "tool_calls" : "stop",
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
    return { fetch: fetchFn, seenModels };
  }

  it("retries on v4-pro when flash outputs the NEEDS_PRO marker as lead-in", async () => {
    const { fetch, seenModels } = modelCapturingFetch([
      { content: "<<<NEEDS_PRO>>>" }, // first call on flash → escalation request
      { content: "OK, here's the answer on pro." }, // retry on pro → real response
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-flash",
      stream: false,
    });

    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("hard question")) {
      events.push({ role: ev.role, content: ev.content });
    }

    // Two model calls total: first flash, second pro
    expect(seenModels).toEqual(["deepseek-v4-flash", "deepseek-v4-pro"]);
    // A warning surfaced about the retry
    expect(events.some((e) => e.role === "warning" && /escalat/i.test(e.content ?? ""))).toBe(true);
    // The final assistant message is the pro-generated content, not the marker
    const finalEv = events.find((e) => e.role === "assistant_final");
    expect(finalEv?.content).toBe("OK, here's the answer on pro.");
  });

  it("does not retry when the response merely mentions the marker mid-text", async () => {
    const { fetch, seenModels } = modelCapturingFetch([
      { content: "See the docs: <<<NEEDS_PRO>>> is a reserved marker." },
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-flash",
      stream: false,
    });

    for await (const _ev of loop.step("explain the marker")) {
      /* drain */
    }

    expect(seenModels).toEqual(["deepseek-v4-flash"]);
  });

  it("does not escalate again when the model is already on pro", async () => {
    // Even if pro happens to echo the marker, no infinite-retry loop.
    const { fetch, seenModels } = modelCapturingFetch([
      { content: "<<<NEEDS_PRO>>>" }, // on pro — should NOT trigger retry
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-pro",
      stream: false,
    });

    for await (const _ev of loop.step("hi")) {
      /* drain */
    }

    // Exactly one call; no retry.
    expect(seenModels).toEqual(["deepseek-v4-pro"]);
  });

  it("surfaces the model's reason in the warning when marker carries one", async () => {
    const { fetch } = modelCapturingFetch([
      { content: "<<<NEEDS_PRO: cross-file refactor with circular imports>>>" },
      { content: "Done on pro." },
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    const events: { role: string; content?: string }[] = [];
    for await (const ev of loop.step("refactor this")) {
      events.push({ role: ev.role, content: ev.content });
    }
    const warning = events.find((e) => e.role === "warning" && /escalat/i.test(e.content ?? ""));
    expect(warning).toBeDefined();
    expect(warning?.content).toContain("cross-file refactor with circular imports");
  });

  it("treats an empty reason payload the same as the bare marker", async () => {
    const { fetch, seenModels } = modelCapturingFetch([
      { content: "<<<NEEDS_PRO: >>>" }, // empty reason
      { content: "Done on pro." },
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    for await (const _ev of loop.step("x")) {
      /* drain */
    }
    expect(seenModels).toEqual(["deepseek-v4-flash", "deepseek-v4-pro"]);
  });

  it("does not match a malformed marker (no closing >>>)", async () => {
    const { fetch, seenModels } = modelCapturingFetch([
      { content: "<<<NEEDS_PRO: this looks like a marker but never closes" },
    ]);
    const loop = new CacheFirstLoop({
      client: new DeepSeekClient({ apiKey: "sk-test", fetch }),
      prefix: new ImmutablePrefix({ system: "be brief" }),
      model: "deepseek-v4-flash",
      stream: false,
    });
    for await (const _ev of loop.step("x")) {
      /* drain */
    }
    // No retry — the marker never closed, so the content streams as-is.
    expect(seenModels).toEqual(["deepseek-v4-flash"]);
  });
});

describe("CacheFirstLoop (streaming) — tool_call_delta emission", () => {
  it("yields tool_call_delta events carrying growing arg-char count", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      // Fake fetch that streams an SSE body with a multi-chunk tool call.
      fetch: (async (_url: any, _init: any) => {
        const frames = [
          `data: ${JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, id: "c1", function: {} }] } }] })}\n\n`,
          `data: ${JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, function: { name: "edit_file", arguments: '{"path":"a.txt","search":"' } }] } }] })}\n\n`,
          `data: ${JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: 'old","replace":"new"}' } }] } }] })}\n\n`,
          `data: ${JSON.stringify({ choices: [{ finish_reason: "tool_calls", delta: {} }], usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2, prompt_cache_hit_tokens: 0, prompt_cache_miss_tokens: 1 } })}\n\n`,
          "data: [DONE]\n\n",
        ];
        const body = new ReadableStream({
          start(ctrl) {
            for (const f of frames) ctrl.enqueue(new TextEncoder().encode(f));
            ctrl.close();
          },
        });
        return new Response(body, {
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
        });
      }) as unknown as typeof fetch,
    });

    const tools = new ToolRegistry();
    tools.register({
      name: "edit_file",
      parameters: { type: "object", properties: {}, required: [] },
      fn: () => "ok",
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: true,
      maxToolIters: 1,
    });

    const deltas: Array<{ name?: string; chars?: number }> = [];
    for await (const ev of loop.step("do it")) {
      if (ev.role === "tool_call_delta") {
        deltas.push({ name: ev.toolName, chars: ev.toolCallArgsChars });
      }
      if (ev.role === "tool_start") break;
    }

    expect(deltas.length).toBeGreaterThanOrEqual(2);
    expect(deltas[0]!.name).toBe("edit_file");
    expect(deltas[deltas.length - 1]!.chars).toBeGreaterThan(deltas[0]!.chars!);
  });

  it("does not emit a red error event when the API call is aborted mid-flight", async () => {
    // Reproduces the reported "error This operation was aborted" UX
    // bug: when App.tsx calls loop.abort() to switch to a queued
    // synthetic input (e.g. ShellConfirm "always allow"), the in-flight
    // fetch throws AbortError. We treat that as a clean early-exit
    // (yield `done`) instead of bubbling it up as a red error row.
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      // Slow fake fetch — never resolves on its own; only the abort
      // signal terminates it.
      fetch: vi.fn(async (_url: any, init: any) => {
        const signal: AbortSignal | undefined = init?.signal;
        return new Promise<Response>((_resolve, reject) => {
          signal?.addEventListener("abort", () =>
            reject(new DOMException("This operation was aborted", "AbortError")),
          );
        });
      }) as any,
      retry: { maxAttempts: 1 },
    });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });

    const events: Array<{ role: string; error?: string }> = [];
    const stepPromise = (async () => {
      for await (const ev of loop.step("hi")) {
        events.push({ role: ev.role, error: ev.error });
      }
    })();
    // Race: fire abort before the fake fetch can resolve.
    setTimeout(() => loop.abort(), 10);
    await stepPromise;

    // No "error" event leaked through.
    expect(events.find((e) => e.role === "error")).toBeUndefined();
    // Loop terminated cleanly so the TUI's busy state unsticks.
    expect(events[events.length - 1]?.role).toBe("done");
  });

  it.skip("defers sibling tool calls when change_workspace pops the confirmation modal", async () => {
    // This test is skipped — change_workspace was removed (fb1b306).
    // The model emits TWO tool calls in one assistant message:
    // change_workspace + write_file. The workspace switch needs user
    // approval; the write must NOT execute against the OLD root before
    // the user confirms (silent data loss). Both still get tool
    // results — the deferred one with a clear "skipped" payload — so
    // tool_call ↔ tool pairing stays valid for DeepSeek's next turn.
    const { registerWorkspaceTool } = await import("../src/tools/workspace.js");

    const client = makeClient([
      {
        content: "",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: {
              name: "change_workspace",
              arguments: `{"path":"${process.cwd().replace(/\\/g, "/")}"}`,
            },
          },
          {
            id: "call_2",
            type: "function",
            function: {
              name: "write_marker",
              arguments: '{"value":"should-not-fire"}',
            },
          },
        ],
      },
      { content: "ok" },
    ]);

    const tools = new ToolRegistry();
    registerWorkspaceTool(tools);
    let writeFired = false;
    tools.register<{ value: string }, string>({
      name: "write_marker",
      parameters: { type: "object", properties: { value: { type: "string" } } },
      fn: ({ value }) => {
        writeFired = true;
        return value;
      },
    });

    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
    });

    const toolResults: Array<{ name: string; content: string }> = [];
    for await (const ev of loop.step("switch and write")) {
      if (ev.role === "tool" && ev.toolName) {
        toolResults.push({ name: ev.toolName, content: ev.content });
      }
    }

    expect(writeFired).toBe(false);
    expect(toolResults).toHaveLength(2);
    expect(toolResults[0]?.name).toBe("change_workspace");
    expect(toolResults[0]?.content).toContain("WorkspaceConfirmationError");
    expect(toolResults[1]?.name).toBe("write_marker");
    expect(toolResults[1]?.content).toContain("deferred");
  });

  it("blocks on confirmation gate without letting the model retry in the same turn", async () => {
    // An auto-approving gate so the tool doesn't block forever in tests.
    // In production, the singleton gate shows the ShellConfirm modal.
    const gate = new PauseGate();
    // Override ask to auto-approve without blocking.
    const origAsk = gate.ask.bind(gate);
    void origAsk;
    gate.ask = (_opts: { kind: string; payload?: unknown }) => {
      return Promise.resolve<ConfirmationChoice>({ type: "run_once" });
    };

    // A tool that uses the confirmation gate (like run_command does)
    const reg = new ToolRegistry();
    reg.register({
      name: "run_command",
      description: "run a command — needs confirmation",
      parameters: {
        type: "object",
        properties: { command: { type: "string" } },
        required: ["command"],
      },
      fn: async (_args: { command: string }, ctx) => {
        // Simulate what shell.ts does: block on the gate
        const realGate = ctx?.confirmationGate ?? gate;
        const choice = await (realGate.ask({
          kind: "run_command",
          payload: { command: "echo ok" },
        }) as Promise<ConfirmationChoice>);
        if (choice.type === "deny") {
          throw new Error("user denied: echo ok");
        }
        return "$ echo ok\n[exit 0]\nok";
      },
    });

    // Response 1: model emits a run_command tool call
    const toolCallResp: FakeResponseShape = {
      content: "",
      tool_calls: [
        {
          id: "call_1",
          type: "function",
          function: {
            name: "run_command",
            arguments: '{"command":"echo ok"}',
          },
        },
      ],
    };
    // Response 2: model sees the tool output and responds naturally
    const followUpResp: FakeResponseShape = {
      content: "Command ran successfully — output was 'ok'.",
      tool_calls: [],
    };

    const responses: FakeResponseShape[] = [toolCallResp, followUpResp];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fakeFetch(responses) as unknown as typeof fetch,
    });

    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: reg.specs() }),
      tools: reg,
      stream: false,
      confirmationGate: gate,
    });

    const events: Array<{ role: string; content?: string }> = [];
    for await (const ev of loop.step("run something")) {
      events.push({ role: ev.role, content: ev.content });
    }

    // The tool result should be the normal command output — not a
    // NeedsConfirmationError string
    const toolEvents = events.filter((e) => e.role === "tool");
    expect(toolEvents).toHaveLength(1);
    expect(toolEvents[0]?.content).toContain("ok");
    expect(toolEvents[0]?.content).not.toContain("NeedsConfirmationError");
    expect(toolEvents[0]?.content).not.toContain("user denied");

    // Two model calls: first generates the tool call, second responds to the
    // output. The gate made the tool return real output synchronously — no
    // error, no NeedsConfirmationError, no synthetic retry.
    const finals = events.filter((e) => e.role === "assistant_final");
    expect(finals).toHaveLength(2);
    // Second call should be the natural follow-up, not a workaround
    expect(finals[1]?.content).toMatch(/ran successfully/);

    // Turn ends cleanly
    expect(events[events.length - 1]?.role).toBe("done");
  });

  describe("session USD budget", () => {
    // The gate runs purely against `loop.stats.totalCost`, which sums
    // the public `turns` array. Tests inject synthetic turns directly
    // instead of pumping fake API responses sized to land in the
    // narrow 80%-100% window — keeps each case focused on the
    // gate's behavior without coupling to v4-flash token pricing.
    function injectCost(loop: CacheFirstLoop, costUsd: number): void {
      // SessionStats.turns is `readonly` at the type level (you can't
      // reassign the field), but the array itself is mutable — the
      // public API normally appends via recordTurn(). For tests we
      // bypass that path; the only fields the gate reads are summed
      // via `t.cost`, so the rest is filler.
      (loop.stats.turns as unknown as Array<{ cost: number; model: string; usage: unknown }>).push({
        cost: costUsd,
        model: loop.model,
        usage: {
          promptTokens: 0,
          completionTokens: 0,
          totalTokens: 0,
          promptCacheHitTokens: 0,
          promptCacheMissTokens: 0,
          cacheHitRatio: 0,
        },
      });
    }

    it("default behavior: budgetUsd undefined → no checks, no events", async () => {
      const client = makeClient([{ content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        // no budgetUsd
      });
      expect(loop.budgetUsd).toBeNull();
      injectCost(loop, 9999); // even huge fake spend doesn't matter
      const roles: string[] = [];
      for await (const ev of loop.step("q")) roles.push(ev.role);
      expect(roles).not.toContain("error");
      expect(roles.filter((r) => r === "warning")).toHaveLength(0);
    });

    it("warns once when cumulative cost crosses 80% of the cap", async () => {
      const client = makeClient([{ content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        budgetUsd: 1.0,
      });
      injectCost(loop, 0.85); // 85% of cap

      const roles: string[] = [];
      const warnings: string[] = [];
      for await (const ev of loop.step("a")) {
        roles.push(ev.role);
        if (ev.role === "warning") warnings.push(ev.content);
      }
      expect(warnings.filter((w) => /budget 80% used/.test(w))).toHaveLength(1);
      expect(roles).toContain("assistant_final");
    });

    it("does not repeat the 80% warning on subsequent turns", async () => {
      const client = makeClient([{ content: "ok" }, { content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        budgetUsd: 1.0,
      });
      injectCost(loop, 0.85);
      // Turn 1 fires warn.
      let turn1Warns = 0;
      for await (const ev of loop.step("a")) {
        if (ev.role === "warning" && /budget/.test(ev.content)) turn1Warns++;
      }
      // Turn 2 starts at the same 0.85 spent (real turn cost is tiny
      // with our fake fetch's default 100/20 token usage) — gate still
      // sees >80% but the 80%-warning latch is sticky, so no repeat.
      let turn2Warns = 0;
      for await (const ev of loop.step("b")) {
        if (ev.role === "warning" && /budget/.test(ev.content)) turn2Warns++;
      }
      expect(turn1Warns).toBe(1);
      expect(turn2Warns).toBe(0);
    });

    it("refuses the next turn once cumulative cost ≥ cap", async () => {
      const client = makeClient([{ content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        budgetUsd: 1.0,
      });
      injectCost(loop, 1.5);

      const events: { role: string; error?: string }[] = [];
      for await (const ev of loop.step("a")) {
        events.push({ role: ev.role, error: ev.error });
      }
      expect(events).toHaveLength(1);
      expect(events[0]?.role).toBe("error");
      expect(events[0]?.error).toMatch(/budget exhausted/);
      // Gate runs before any state mutation: only the injected fake
      // turn remains, no real model call recorded.
      expect(loop.stats.turns).toHaveLength(1);
    });

    it("setBudget(null) clears the cap and unblocks subsequent turns", async () => {
      const client = makeClient([{ content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        budgetUsd: 1.0,
      });
      injectCost(loop, 1.5);
      // Sanity check: the cap is currently exhausted.
      let firstAttempt: string | null = null;
      for await (const ev of loop.step("a")) {
        if (ev.role === "error") firstAttempt = ev.error ?? "";
        break;
      }
      expect(firstAttempt).toMatch(/budget exhausted/);
      // Clear the cap and try again.
      loop.setBudget(null);
      const roles: string[] = [];
      for await (const ev of loop.step("a")) roles.push(ev.role);
      expect(roles).not.toContain("error");
      expect(roles).toContain("assistant_final");
    });

    it("setBudget re-arms the 80% warning when the cap moves", async () => {
      const client = makeClient([{ content: "ok" }, { content: "ok" }]);
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s" }),
        stream: false,
        budgetUsd: 1.0,
      });
      injectCost(loop, 0.85); // 85% of $1
      // Turn 1: warn fires (sticky after this).
      let turn1Warns = 0;
      for await (const ev of loop.step("a")) {
        if (ev.role === "warning" && /budget/.test(ev.content)) turn1Warns++;
      }
      expect(turn1Warns).toBe(1);
      // Lower the cap further so spent (0.85) is even further past
      // the new 80% mark. setBudget must reset the sticky flag so
      // the user sees a fresh warning at the new threshold.
      loop.setBudget(0.95);
      let turn2Warns = 0;
      for await (const ev of loop.step("b")) {
        if (ev.role === "warning" && /budget/.test(ev.content)) turn2Warns++;
      }
      expect(turn2Warns).toBe(1);
    });
  });

  describe("parallel tool dispatch", () => {
    function makeMultiToolResponse(calls: Array<{ name: string; args: string }>) {
      return {
        content: "",
        tool_calls: calls.map((c, i) => ({
          id: `call_${i}`,
          type: "function",
          function: { name: c.name, arguments: c.args },
        })),
      };
    }

    it("runs consecutive parallelSafe calls concurrently", async () => {
      const client = makeClient([
        makeMultiToolResponse([
          { name: "slow_read", args: '{"k":1}' },
          { name: "slow_read", args: '{"k":2}' },
          { name: "slow_read", args: '{"k":3}' },
        ]),
        { content: "ok" },
      ]);
      const tools = new ToolRegistry();
      tools.register({
        name: "slow_read",
        parallelSafe: true,
        fn: async (args: { k: number }) => {
          await new Promise((r) => setTimeout(r, 80));
          return String(args.k);
        },
      });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
        tools,
        stream: false,
      });

      const t0 = Date.now();
      for await (const _ of loop.step("go")) {
        // drain
      }
      const elapsed = Date.now() - t0;

      expect(elapsed).toBeLessThan(220);
    });

    it("unsafe call splits the chunk into serial barriers", async () => {
      const client = makeClient([
        makeMultiToolResponse([
          { name: "slow_read", args: '{"k":1}' },
          { name: "slow_write", args: '{"k":2}' },
          { name: "slow_read", args: '{"k":3}' },
        ]),
        { content: "ok" },
      ]);
      const tools = new ToolRegistry();
      tools.register({
        name: "slow_read",
        parallelSafe: true,
        fn: async () => {
          await new Promise((r) => setTimeout(r, 80));
          return "r";
        },
      });
      tools.register({
        name: "slow_write",
        fn: async () => {
          await new Promise((r) => setTimeout(r, 80));
          return "w";
        },
      });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
        tools,
        stream: false,
      });

      const t0 = Date.now();
      for await (const _ of loop.step("go")) {
        // drain
      }
      const elapsed = Date.now() - t0;

      expect(elapsed).toBeGreaterThan(220);
    });

    it("tool yields land in declared order even when later calls finish first", async () => {
      const client = makeClient([
        makeMultiToolResponse([
          { name: "delayed", args: '{"id":"a","ms":120}' },
          { name: "delayed", args: '{"id":"b","ms":20}' },
          { name: "delayed", args: '{"id":"c","ms":60}' },
        ]),
        { content: "ok" },
      ]);
      const tools = new ToolRegistry();
      tools.register({
        name: "delayed",
        parallelSafe: true,
        fn: async (args: { id: string; ms: number }) => {
          await new Promise((r) => setTimeout(r, args.ms));
          return args.id;
        },
      });
      const loop = new CacheFirstLoop({
        client,
        prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
        tools,
        stream: false,
      });

      const order: string[] = [];
      for await (const ev of loop.step("go")) {
        if (ev.role === "tool") order.push(ev.content);
      }
      expect(order).toEqual(["a", "b", "c"]);
    });

    it("REASONIX_TOOL_DISPATCH=serial forces serial dispatch", async () => {
      const prev = process.env.REASONIX_TOOL_DISPATCH;
      process.env.REASONIX_TOOL_DISPATCH = "serial";
      try {
        const client = makeClient([
          makeMultiToolResponse([
            { name: "slow_read", args: '{"k":1}' },
            { name: "slow_read", args: '{"k":2}' },
          ]),
          { content: "ok" },
        ]);
        const tools = new ToolRegistry();
        tools.register({
          name: "slow_read",
          parallelSafe: true,
          fn: async () => {
            await new Promise((r) => setTimeout(r, 80));
            return "x";
          },
        });
        const loop = new CacheFirstLoop({
          client,
          prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
          tools,
          stream: false,
        });

        const t0 = Date.now();
        for await (const _ of loop.step("go")) {
          // drain
        }
        const elapsed = Date.now() - t0;

        expect(elapsed).toBeGreaterThan(150);
      } finally {
        if (prev === undefined) {
          // biome-ignore lint/performance/noDelete: env restore must remove the key, not stringify "undefined"
          delete process.env.REASONIX_TOOL_DISPATCH;
        } else process.env.REASONIX_TOOL_DISPATCH = prev;
      }
    });

    it("REASONIX_PARALLEL_MAX caps the chunk size", async () => {
      const prev = process.env.REASONIX_PARALLEL_MAX;
      process.env.REASONIX_PARALLEL_MAX = "2";
      try {
        const client = makeClient([
          makeMultiToolResponse([
            { name: "slow_read", args: '{"k":1}' },
            { name: "slow_read", args: '{"k":2}' },
            { name: "slow_read", args: '{"k":3}' },
            { name: "slow_read", args: '{"k":4}' },
          ]),
          { content: "ok" },
        ]);
        const tools = new ToolRegistry();
        tools.register({
          name: "slow_read",
          parallelSafe: true,
          fn: async () => {
            await new Promise((r) => setTimeout(r, 80));
            return "x";
          },
        });
        const loop = new CacheFirstLoop({
          client,
          prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
          tools,
          stream: false,
        });

        const t0 = Date.now();
        for await (const _ of loop.step("go")) {
          // drain
        }
        const elapsed = Date.now() - t0;

        expect(elapsed).toBeGreaterThan(150);
        expect(elapsed).toBeLessThan(280);
      } finally {
        if (prev === undefined) {
          // biome-ignore lint/performance/noDelete: env restore must remove the key, not stringify "undefined"
          delete process.env.REASONIX_PARALLEL_MAX;
        } else process.env.REASONIX_PARALLEL_MAX = prev;
      }
    });
  });
});
