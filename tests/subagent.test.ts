/** Subagent tool — registration, child-loop isolation, fork-registry exclusion, abort propagation, plan-mode inheritance. */

import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient, Usage } from "../src/client.js";
import { ToolRegistry } from "../src/tools.js";
import {
  type SubagentEvent,
  type SubagentResult,
  type SubagentSink,
  forkRegistryExcluding,
  forkRegistryWithAllowList,
  formatSubagentResult,
  registerSubagentTool,
  spawnSubagent,
  subagentBudgetHint,
} from "../src/tools/subagent.js";

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
    const usage = resp.usage ?? {
      prompt_tokens: 100,
      completion_tokens: 20,
      total_tokens: 120,
      prompt_cache_hit_tokens: 0,
      prompt_cache_miss_tokens: 100,
    };
    if (body.stream === true) {
      const finish = resp.tool_calls ? "tool_calls" : "stop";
      const delta: Record<string, unknown> = {};
      if (resp.content) delta.content = resp.content;
      if (resp.reasoning_content) delta.reasoning_content = resp.reasoning_content;
      if (resp.tool_calls) delta.tool_calls = resp.tool_calls;
      const frames = [
        `data: ${JSON.stringify({ choices: [{ index: 0, delta }] })}\n\n`,
        `data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: finish }], usage })}\n\n`,
        "data: [DONE]\n\n",
      ];
      return new Response(new TextEncoder().encode(frames.join("")), {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      });
    }
    return new Response(
      JSON.stringify({
        _echo_messages: body.messages,
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
        usage,
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

function makeToolCallResponses(n: number): FakeResponseShape[] {
  return Array.from({ length: n }, (_, i) => ({
    content: "",
    tool_calls: [
      {
        id: `call_${i + 1}`,
        type: "function",
        function: { name: "noop", arguments: JSON.stringify({ i: i + 1 }) },
      },
    ],
  }));
}

function makeSink(): { sink: SubagentSink; events: SubagentEvent[] } {
  const events: SubagentEvent[] = [];
  const sink: SubagentSink = {
    current: (ev) => {
      events.push(ev);
    },
  };
  return { sink, events };
}

describe("registerSubagentTool", () => {
  it("registers spawn_subagent into the parent registry", () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "ok" }]);
    registerSubagentTool(parent, { client });
    expect(parent.has("spawn_subagent")).toBe(true);
  });

  it("returns a structured success payload with the subagent's final answer", async () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "the answer is 42" }]);
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "what is the answer?" }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.success).toBe(true);
    expect(parsed.output).toBe("the answer is 42");
    expect(parsed.turns).toBe(1);
    expect(parsed.tool_iters).toBe(0);
    expect(typeof parsed.elapsed_ms).toBe("number");
  });

  it("rejects an empty task with a structured error", async () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "won't be called" }]);
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "   \n  " }));
    const parsed = JSON.parse(out);
    expect(parsed.error).toMatch(/non-empty 'task'/);
  });

  it("emits start → end events through the sink", async () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "done" }]);
    const { sink, events } = makeSink();
    registerSubagentTool(parent, { client, sink });
    await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "this task is over thirty characters long" }),
    );
    expect(events[0]?.kind).toBe("start");
    expect(events[events.length - 1]?.kind).toBe("end");
    // task preview truncated to 30 chars + ellipsis
    expect(events[0]?.task).toMatch(/…$/);
    expect(events[0]?.task.length).toBeLessThanOrEqual(31);
    // end event carries the summary + turn count
    const end = events[events.length - 1]!;
    expect(end.summary).toBe("done");
    expect(end.turns).toBe(1);
    expect(end.error).toBeUndefined();
    // 0.5.14: end event also carries cost, model, and aggregate usage
    // so the sink can write a subagent row to the usage log without
    // recomputing anything.
    expect(end.model).toBeTruthy();
    expect(end.usage).toBeDefined();
    expect(end.usage?.promptTokens).toBeGreaterThan(0);
    expect(end.costUsd).toBeGreaterThan(0);
  });

  it("emits a progress event for each tool result inside the child loop", async () => {
    const parent = new ToolRegistry();
    parent.register({
      name: "noop",
      readOnly: true,
      fn: () => "noop-result",
    });
    const client = makeClient([
      {
        content: "",
        tool_calls: [
          {
            id: "call_1",
            type: "function",
            function: { name: "noop", arguments: "{}" },
          },
        ],
      },
      { content: "all done" },
    ]);
    const { sink, events } = makeSink();
    registerSubagentTool(parent, { client, sink });
    await parent.dispatch("spawn_subagent", JSON.stringify({ task: "use noop" }));
    const progress = events.filter((e) => e.kind === "progress");
    expect(progress.length).toBe(1);
    expect(progress[0]?.iter).toBe(1);
  });

  it("surfaces a child-loop error in the structured result + end event", async () => {
    const parent = new ToolRegistry();
    // 401 from the fake fetch → DeepSeekClient throws inside the child step()
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async () => new Response("unauthorized", { status: 401 })) as any,
      retry: { maxAttempts: 1 },
    });
    const { sink, events } = makeSink();
    registerSubagentTool(parent, { client, sink });
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "fail please" }));
    const parsed = JSON.parse(out);
    expect(parsed.success).toBe(false);
    expect(parsed.error).toBeTruthy();
    const end = events[events.length - 1]!;
    expect(end.kind).toBe("end");
    expect(end.error).toBeTruthy();
    expect(end.summary).toBeUndefined();
  });

  it("truncates oversized output and signals the truncation", async () => {
    const parent = new ToolRegistry();
    const huge = "x".repeat(20_000);
    const client = makeClient([{ content: huge }]);
    registerSubagentTool(parent, { client, maxResultChars: 100 });
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "spew" }));
    const parsed = JSON.parse(out);
    expect(parsed.output.length).toBeLessThan(huge.length);
    expect(parsed.output).toMatch(/truncated/);
  });

  it("never registers spawn_subagent itself into the child registry (no recursion)", async () => {
    // We can't easily peek at the child registry from outside the tool,
    // but we CAN observe the child loop's prefix.toolSpecs via the
    // request body the fake fetch sees. Tools advertised in the request
    // are exactly the child registry's specs.
    const parent = new ToolRegistry();
    parent.register({ name: "harmless", readOnly: true, fn: () => "ok" });
    parent.register({ name: "submit_plan", readOnly: true, fn: () => "ok" });
    const seenToolNames: string[][] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        const tools = (body.tools ?? []) as Array<{ function: { name: string } }>;
        seenToolNames.push(tools.map((t) => t.function.name));
        return new Response(
          JSON.stringify({
            choices: [
              {
                index: 0,
                message: { role: "assistant", content: "fine" },
                finish_reason: "stop",
              },
            ],
            usage: { prompt_tokens: 10, completion_tokens: 1, total_tokens: 11 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
    });
    registerSubagentTool(parent, { client });
    await parent.dispatch("spawn_subagent", JSON.stringify({ task: "go" }));
    expect(seenToolNames.length).toBe(1);
    const childTools = seenToolNames[0]!;
    // Inherited the harmless tool, but NOT spawn_subagent or submit_plan.
    expect(childTools).toContain("harmless");
    expect(childTools).not.toContain("spawn_subagent");
    expect(childTools).not.toContain("submit_plan");
  });

  it("respects a custom system prompt passed in the tool args", async () => {
    const seenSystems: string[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        const sys = (body.messages ?? []).find((m: any) => m.role === "system");
        if (sys) seenSystems.push(sys.content);
        return new Response(
          JSON.stringify({
            choices: [
              {
                index: 0,
                message: { role: "assistant", content: "ok" },
                finish_reason: "stop",
              },
            ],
            usage: { prompt_tokens: 10, completion_tokens: 1, total_tokens: 11 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
    });
    const parent = new ToolRegistry();
    registerSubagentTool(parent, { client });
    await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "go", system: "You are a custom subagent." }),
    );
    expect(seenSystems[0]).toContain("You are a custom subagent.");
    expect(seenSystems[0]).toMatch(/Tool budget: you have \d+ tool calls/);
  });

  it("falls back to the default model when the model arg is invalid", async () => {
    const seenModels: string[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        seenModels.push(body.model);
        return new Response(
          JSON.stringify({
            choices: [
              { index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" },
            ],
            usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
    });
    const parent = new ToolRegistry();
    registerSubagentTool(parent, { client });
    // "gpt-4" is not a deepseek-* model — should be ignored.
    await parent.dispatch("spawn_subagent", JSON.stringify({ task: "go", model: "gpt-4" }));
    // Subagent default was pro pre-0.6; now flash to keep explore/research
    // cheap. Skill frontmatter `model:` is the opt-in override for skills
    // that empirically benefit from pro.
    expect(seenModels[0]).toBe("deepseek-v4-flash");
  });

  it("aborts the child when the parent's tool ctx signal fires", async () => {
    const parent = new ToolRegistry();
    // Slow client — sleeps 200ms before responding so the abort beats it.
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const signal: AbortSignal | undefined = init?.signal;
        await new Promise<void>((resolve, reject) => {
          const t = setTimeout(resolve, 200);
          signal?.addEventListener("abort", () => {
            clearTimeout(t);
            reject(new DOMException("aborted", "AbortError"));
          });
        });
        return new Response(
          JSON.stringify({
            choices: [
              { index: 0, message: { role: "assistant", content: "late" }, finish_reason: "stop" },
            ],
            usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
      retry: { maxAttempts: 1 },
    });
    registerSubagentTool(parent, { client });
    const ctrl = new AbortController();
    setTimeout(() => ctrl.abort(), 20);
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "slow" }), {
      signal: ctrl.signal,
    });
    const parsed = JSON.parse(out);
    expect(parsed.success).toBe(false);
  });

  it("honors a parentSignal that was already aborted at dispatch time", async () => {
    // Race we previously dropped on the floor: parent.abort() fires
    // before spawn_subagent's listener attach runs. addEventListener
    // doesn't replay abort events for already-aborted signals, so the
    // listener stayed silent forever and the child ran free until it
    // hit its iter budget. Fix: synchronously check `.aborted` at
    // attach and forward immediately to childLoop.abort(), and have
    // step() carry the aborted state across its _turnAbort reset.
    const parent = new ToolRegistry();
    let fetchCalls = 0;
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      // If the abort propagation works, fetch is never called — the
      // child loop bails at iter 0 because its signal is already
      // aborted before the API call site is reached.
      fetch: vi.fn(async () => {
        fetchCalls++;
        return new Response(
          JSON.stringify({
            choices: [
              { index: 0, message: { role: "assistant", content: "late" }, finish_reason: "stop" },
            ],
            usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
      retry: { maxAttempts: 1 },
    });
    registerSubagentTool(parent, { client });
    const ctrl = new AbortController();
    ctrl.abort(); // already aborted before dispatch is even called
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "x" }), {
      signal: ctrl.signal,
    });
    const parsed = JSON.parse(out);
    expect(parsed.success).toBe(false);
    expect(fetchCalls).toBe(0);
  });

  it("exposes max_iters in the tool schema without an upper bound (checkpoint cadence, not budget)", () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "ok" }]);
    registerSubagentTool(parent, { client });
    const spec = parent.specs().find((s) => s.function.name === "spawn_subagent");
    const params = spec?.function.parameters as {
      properties: { max_iters: { type: string; minimum?: number; maximum?: number } };
    };
    expect(params.properties.max_iters.type).toBe("integer");
    expect(params.properties.max_iters.maximum).toBeUndefined();
  });

  it("paces the child loop at the caller-supplied max_iters (pauses on reaching it)", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(50));
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "loop forever", max_iters: 3 }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(3);
    expect(parsed.paused).toBe(true);
    expect(typeof parsed.resume_session).toBe("string");
  });

  it("paused result carries partial_summary so the parent can decide informed", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const responses: FakeResponseShape[] = [
      ...makeToolCallResponses(2),
      {
        content:
          "Read 2 files, mapped the auth flow; still need to wire the refresh-token path; blocked on the token-store interface choice.",
      },
    ];
    const client = makeClient(responses);
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "trace the auth flow", max_iters: 2 }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.paused).toBe(true);
    expect(parsed.partial_summary).toMatch(/Read 2 files/);
    expect(parsed.partial_summary).toMatch(/blocked/);
  });

  it("paused result still works when the summary call returns empty content", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const responses: FakeResponseShape[] = [...makeToolCallResponses(2), { content: "" }];
    const client = makeClient(responses);
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "loop forever", max_iters: 2 }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.paused).toBe(true);
    expect(parsed.partial_summary).toBeUndefined();
  });

  it("passes large max_iters values through without clamping", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(60));
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "loop forever", max_iters: 50 }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(50);
    expect(parsed.paused).toBe(true);
  });

  it("ignores non-numeric max_iters and falls back to the default", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(50));
    registerSubagentTool(parent, { client, maxToolIters: 4 });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "loop forever", max_iters: "lots" }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(4);
  });

  it("type=explore uses the explore persona and 20-iter budget", async () => {
    const seenSystems: string[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        const sys = (body.messages ?? []).find((m: any) => m.role === "system");
        if (sys) seenSystems.push(sys.content);
        return new Response(
          JSON.stringify({
            choices: [
              { index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" },
            ],
            usage: { prompt_tokens: 10, completion_tokens: 1, total_tokens: 11 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
    });
    const parent = new ToolRegistry();
    registerSubagentTool(parent, { client });
    await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "find all callers of foo()", type: "explore" }),
    );
    expect(seenSystems[0]).toMatch(/exploration subagent/);
  });

  it("type=verify caps the child loop at the verify budget (8)", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(50));
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "verify foo", type: "verify" }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(8);
  });

  it("explicit max_iters overrides the type's default budget", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(50));
    registerSubagentTool(parent, { client });
    const out = await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "verify foo", type: "verify", max_iters: 4 }),
    );
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(4);
  });

  it("explicit system overrides the type's default prompt", async () => {
    const seenSystems: string[] = [];
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async (_url: any, init: any) => {
        const body = init?.body ? JSON.parse(init.body) : {};
        const sys = (body.messages ?? []).find((m: any) => m.role === "system");
        if (sys) seenSystems.push(sys.content);
        return new Response(
          JSON.stringify({
            choices: [
              { index: 0, message: { role: "assistant", content: "ok" }, finish_reason: "stop" },
            ],
            usage: { prompt_tokens: 10, completion_tokens: 1, total_tokens: 11 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }) as any,
    });
    const parent = new ToolRegistry();
    registerSubagentTool(parent, { client });
    await parent.dispatch(
      "spawn_subagent",
      JSON.stringify({ task: "go", type: "explore", system: "I am a custom prompt." }),
    );
    expect(seenSystems[0]).toContain("I am a custom prompt.");
    expect(seenSystems[0]).toMatch(/Tool budget: you have 20 tool calls/);
  });

  it("omitted type leaves the default persona and budget unchanged", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient(makeToolCallResponses(50));
    registerSubagentTool(parent, { client, maxToolIters: 5 });
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "loop please" }));
    const parsed = JSON.parse(out);
    expect(parsed.tool_iters).toBe(5);
  });

  it("appends a remaining-budget hint to tool results within 3 iters of the cap", async () => {
    // Drive 5 tool calls. The augmenter appends a hint starting at iter 2
    // (remaining=3). After iter 5 the loop pauses; the partial-summary call
    // (one extra no-tools fetch) then sees all 5 results in the session —
    // including the iter-5 "0 of 5 left — finalize NOW" form.
    const responses = makeToolCallResponses(5);
    const innerFetch = fakeFetch(responses);
    const seenToolResults: string[] = [];
    const fetchSpy = vi.fn(async (url: any, init: any) => {
      const body = init?.body ? JSON.parse(init.body) : {};
      for (const m of body.messages ?? []) {
        if (m.role === "tool") seenToolResults.push(m.content);
      }
      return innerFetch(url, init);
    });
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fetchSpy as any,
    });
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    registerSubagentTool(parent, { client, maxToolIters: 5 });
    await parent.dispatch("spawn_subagent", JSON.stringify({ task: "go" }));
    expect(seenToolResults.length).toBeGreaterThanOrEqual(5);
    const unique = Array.from(new Set(seenToolResults));
    expect(unique).toHaveLength(5);
    expect(unique[0]).not.toMatch(/budget:/);
    expect(unique[0]).toBe("noop-result");
    expect(unique[1]).toMatch(/\[budget: 3 of 5 tool calls left/);
    expect(unique[2]).toMatch(/\[budget: 2 of 5 tool calls left/);
    expect(unique[3]).toMatch(/\[budget: 1 of 5 tool call left/);
    expect(unique[4]).toMatch(/\[budget: 0 of 5 tool calls left — finalize NOW/);
  });

  it("fires onSpawnComplete once per dispatch with the full SubagentResult", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient([{ content: "the distilled answer" }]);
    const captured: { output: string; costUsd: number; usage: { completionTokens: number } }[] = [];
    registerSubagentTool(parent, {
      client,
      onSpawnComplete: (result) => {
        captured.push({
          output: result.output,
          costUsd: result.costUsd,
          usage: { completionTokens: result.usage.completionTokens },
        });
      },
    });
    await parent.dispatch("spawn_subagent", JSON.stringify({ task: "say something" }));
    expect(captured).toHaveLength(1);
    expect(captured[0]!.output).toBe("the distilled answer");
    expect(captured[0]!.usage.completionTokens).toBeGreaterThan(0);
  });

  it("does not propagate onSpawnComplete errors out of the spawn tool dispatch", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "noop", readOnly: true, fn: () => "noop-result" });
    const client = makeClient([{ content: "ok" }]);
    registerSubagentTool(parent, {
      client,
      onSpawnComplete: () => {
        throw new Error("telemetry boom");
      },
    });
    const out = await parent.dispatch("spawn_subagent", JSON.stringify({ task: "anything" }));
    const parsed = JSON.parse(out);
    expect(parsed.success).toBe(true);
    expect(parsed.output).toBe("ok");
  });
});

describe("subagentBudgetHint", () => {
  it("stays silent on the first spawn of a session", () => {
    expect(subagentBudgetHint(1, 120)).toBeNull();
  });

  it("emits the soft note from the second spawn through the fourth", () => {
    expect(subagentBudgetHint(2, 240)).toMatch(/\[note: this session has spawned 2 subagents/);
    expect(subagentBudgetHint(4, 480)).toMatch(/\[note: this session has spawned 4 subagents/);
  });

  it("escalates to the strong budget hint past five spawns", () => {
    const out = subagentBudgetHint(5, 600);
    expect(out).toMatch(/\[budget: this session has now spawned 5 subagents/);
    expect(out).toMatch(/parallel fan-out or >10-read context blow-up/);
  });

  it("escalates to the strong hint when cumulative tokens cross 50k even at low spawn count", () => {
    const out = subagentBudgetHint(2, 60_000);
    expect(out).toMatch(
      /\[budget: this session has now spawned 2 subagents totalling 60000 tokens/,
    );
  });
});

describe("registerSubagentTool — per-session budget feedback", () => {
  it("appends nothing on the first spawn, the soft note on the second, and the strong hint on the fifth", async () => {
    const parent = new ToolRegistry();
    const client = makeClient([{ content: "answer" }]);
    registerSubagentTool(parent, { client });

    const outs: string[] = [];
    for (let i = 0; i < 5; i++) {
      outs.push(await parent.dispatch("spawn_subagent", JSON.stringify({ task: `q${i}` })));
    }

    expect(outs[0]).not.toMatch(/\[(note|budget):/);
    expect(outs[1]).toMatch(/\[note: this session has spawned 2 subagents/);
    expect(outs[2]).toMatch(/\[note: this session has spawned 3 subagents/);
    expect(outs[3]).toMatch(/\[note: this session has spawned 4 subagents/);
    expect(outs[4]).toMatch(/\[budget: this session has now spawned 5 subagents/);
  });
});

describe("formatSubagentResult — forcedSummary path", () => {
  function baseResult(over: Partial<SubagentResult>): SubagentResult {
    return {
      success: false,
      output: "",
      turns: 1,
      toolIters: 4,
      elapsedMs: 1000,
      costUsd: 0.0001,
      model: "deepseek-chat",
      usage: new Usage(),
      ...over,
    };
  }

  it("renders forcedSummary results with partial:true and `output` carrying the synthesis", () => {
    const formatted = formatSubagentResult(
      baseResult({
        forcedSummary: true,
        output: "I found X and Y; could not reach Z because the file was truncated.",
      }),
    );
    const parsed = JSON.parse(formatted);
    expect(parsed.success).toBe(false);
    expect(parsed.partial).toBe(true);
    expect(parsed.output).toMatch(/found X and Y/);
    expect(parsed.note).toMatch(/force-summarized/i);
  });

  it("forcedSummary takes precedence over the generic !success branch", () => {
    const formatted = formatSubagentResult(
      baseResult({
        forcedSummary: true,
        output: "partial answer",
        error: "ignored when forcedSummary is set",
      }),
    );
    const parsed = JSON.parse(formatted);
    expect(parsed.partial).toBe(true);
    expect(parsed.output).toBe("partial answer");
    expect(parsed.error).toBeUndefined();
  });

  it("paused still takes precedence over forcedSummary", () => {
    const formatted = formatSubagentResult(
      baseResult({
        paused: true,
        pausedSession: "sub-x-1",
        forcedSummary: true,
        output: "shouldn't appear",
      }),
    );
    const parsed = JSON.parse(formatted);
    expect(parsed.paused).toBe(true);
    expect(parsed.partial).toBeUndefined();
    expect(parsed.output).toBeUndefined();
  });

  it("genuine !success without forcedSummary still uses the error-only shape", () => {
    const formatted = formatSubagentResult(
      baseResult({ error: "subagent ended without producing an answer" }),
    );
    const parsed = JSON.parse(formatted);
    expect(parsed.success).toBe(false);
    expect(parsed.partial).toBeUndefined();
    expect(parsed.error).toMatch(/ended without producing/);
    expect(parsed.output).toBeUndefined();
  });
});

describe("forkRegistryExcluding", () => {
  it("copies all tools except the excluded names", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "a", fn: () => "a" });
    parent.register({ name: "b", fn: () => "b" });
    parent.register({ name: "c", fn: () => "c" });
    const child = forkRegistryExcluding(parent, new Set(["b"]));
    expect(child.has("a")).toBe(true);
    expect(child.has("b")).toBe(false);
    expect(child.has("c")).toBe(true);
    expect(child.size).toBe(2);
  });

  it("propagates plan-mode state from the parent", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "x", readOnly: true, fn: () => "x" });
    parent.setPlanMode(true);
    const child = forkRegistryExcluding(parent, new Set());
    expect(child.planMode).toBe(true);
  });

  it("child registry's plan mode defaults off when parent's is off", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "x", fn: () => "x" });
    const child = forkRegistryExcluding(parent, new Set());
    expect(child.planMode).toBe(false);
  });

  it("dispatching a copied tool still runs its fn", async () => {
    const parent = new ToolRegistry();
    let calls = 0;
    parent.register({
      name: "counter",
      fn: () => {
        calls++;
        return `n=${calls}`;
      },
    });
    const child = forkRegistryExcluding(parent, new Set());
    const out = await child.dispatch("counter", "{}");
    expect(out).toBe("n=1");
  });
});

describe("forkRegistryWithAllowList", () => {
  it("includes only names in the allow-list", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "read" });
    parent.register({ name: "write", fn: () => "write" });
    parent.register({ name: "shell", fn: () => "shell" });
    const child = forkRegistryWithAllowList(parent, new Set(["read", "write"]), new Set());
    expect(child.has("read")).toBe(true);
    expect(child.has("write")).toBe(true);
    expect(child.has("shell")).toBe(false);
    expect(child.size).toBe(2);
  });

  it("alsoExclude wins over allow", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "spawn_subagent", fn: () => "x" });
    parent.register({ name: "read", fn: () => "read" });
    const child = forkRegistryWithAllowList(
      parent,
      new Set(["read", "spawn_subagent"]),
      new Set(["spawn_subagent"]),
    );
    expect(child.has("read")).toBe(true);
    expect(child.has("spawn_subagent")).toBe(false);
  });

  it("propagates parent plan mode", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "x" });
    parent.setPlanMode(true);
    const child = forkRegistryWithAllowList(parent, new Set(["read"]), new Set());
    expect(child.planMode).toBe(true);
  });

  it("ignores allow-list names that are not registered (caller validates)", () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "x" });
    const child = forkRegistryWithAllowList(parent, new Set(["read", "ghost"]), new Set());
    expect(child.has("read")).toBe(true);
    expect(child.has("ghost")).toBe(false);
    expect(child.size).toBe(1);
  });
});

describe("spawnSubagent allowedTools", () => {
  it("scopes the child registry to the allow-list", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "read result" });
    parent.register({ name: "write", fn: () => "write result" });
    parent.register({ name: "shell", fn: () => "shell result" });
    const client = makeClient([{ content: "done" }]);
    const result = await spawnSubagent({
      client,
      parentRegistry: parent,
      system: "test",
      task: "test",
      allowedTools: ["read"],
    });
    expect(result.success).toBe(true);
    expect(parent.has("write")).toBe(true);
    expect(parent.has("shell")).toBe(true);
  });

  it("returns a structured error when allow-list names a tool the parent does not have", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "read result" });
    const client = makeClient([{ content: "should not run" }]);
    const result = await spawnSubagent({
      client,
      parentRegistry: parent,
      system: "test",
      task: "test",
      allowedTools: ["read", "missing_tool"],
    });
    expect(result.success).toBe(false);
    expect(result.error).toContain("missing_tool");
    expect(result.error).toContain("allow-list");
    expect(result.turns).toBe(0);
    expect(result.toolIters).toBe(0);
  });

  it("emits start then end with the validation error and runs no API call", async () => {
    const parent = new ToolRegistry();
    parent.register({ name: "read", fn: () => "read result" });
    const fetchSpy = vi.fn();
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: fetchSpy as unknown as typeof fetch,
    });
    const { sink, events } = makeSink();
    await spawnSubagent({
      client,
      parentRegistry: parent,
      system: "test",
      task: "test",
      allowedTools: ["ghost"],
      sink,
    });
    expect(events.map((e) => e.kind)).toEqual(["start", "end"]);
    expect(events[1]?.error).toContain("ghost");
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
