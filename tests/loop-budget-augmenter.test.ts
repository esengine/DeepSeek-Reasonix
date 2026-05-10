/** Parent-loop budget augmenter — injects a remaining-iter tail into tool results when closing in on the per-turn cap, and leaves a pre-installed augmenter alone (subagent's child-loop case). */

import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { ToolRegistry } from "../src/tools.js";

interface FakeResponseShape {
  content?: string;
  tool_calls?: any[];
  usage?: Record<string, number>;
}

function fakeFetch(responses: FakeResponseShape[]): typeof fetch {
  let i = 0;
  return vi.fn(async (_url: any, _init: any) => {
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
  return new DeepSeekClient({ apiKey: "sk-test", fetch: fakeFetch(responses) });
}

function probeRegistry(): ToolRegistry {
  const reg = new ToolRegistry();
  reg.register({
    name: "probe",
    description: "no-op probe",
    parameters: { type: "object", properties: {} },
    fn: async () => "raw-probe-result",
  });
  return reg;
}

function callProbe(): FakeResponseShape {
  return {
    content: "",
    tool_calls: [{ id: "c", type: "function", function: { name: "probe", arguments: "{}" } }],
  };
}

describe("parent-loop budget augmenter", () => {
  it("does not inject a budget tail when iters remaining > threshold", async () => {
    const tools = probeRegistry();
    const client = makeClient([callProbe(), { content: "done" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 64,
    });

    const toolContents: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool") toolContents.push(ev.content);
    }
    expect(toolContents).toHaveLength(1);
    expect(toolContents[0]).toBe("raw-probe-result");
    expect(toolContents[0]).not.toMatch(/\[budget:/);
  });

  it("injects a remaining-iter tail when iters remaining is at or below threshold", async () => {
    const tools = probeRegistry();
    const client = makeClient([callProbe(), { content: "done" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 5,
    });

    const toolContents: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool") toolContents.push(ev.content);
    }
    expect(toolContents).toHaveLength(1);
    expect(toolContents[0]).toMatch(/^raw-probe-result/);
    expect(toolContents[0]).toMatch(/\[budget: 4 of 5 tool calls left this turn — wrap up soon\]/);
  });

  it("injects the strong stop-now tail when iters remaining is 0 (cap reached)", async () => {
    const tools = probeRegistry();
    const client = makeClient([callProbe(), callProbe(), { content: "done" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 2,
    });

    const toolContents: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool") toolContents.push(ev.content);
    }
    expect(toolContents).toHaveLength(2);
    expect(toolContents[0]).toMatch(/\[budget: 1 of 2 tool calls left this turn — wrap up soon\]/);
    expect(toolContents[1]).toMatch(/\[budget: 0 of 2 tool calls left this turn — finalize NOW/);
  });

  it("resets the per-turn dispatch counter at step entry so consecutive turns don't accumulate", async () => {
    const tools = probeRegistry();
    const client = makeClient([
      callProbe(),
      { content: "turn-1-done" },
      callProbe(),
      { content: "turn-2-done" },
    ]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 5,
    });

    const turn1Tools: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool") turn1Tools.push(ev.content);
    }
    const turn2Tools: string[] = [];
    for await (const ev of loop.step("again")) {
      if (ev.role === "tool") turn2Tools.push(ev.content);
    }
    expect(turn1Tools[0]).toMatch(/\[budget: 4 of 5 tool calls left/);
    expect(turn2Tools[0]).toMatch(/\[budget: 4 of 5 tool calls left/);
  });

  it("preserves a pre-installed augmenter — constructor must not clobber subagent's child-registry augmenter", async () => {
    const tools = probeRegistry();
    tools.setResultAugmenter((_n, _a, result) => `${result}\n\n[child-augmenter-marker]`);
    expect(tools.hasResultAugmenter).toBe(true);

    const client = makeClient([callProbe(), { content: "done" }]);
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s", toolSpecs: tools.specs() }),
      tools,
      stream: false,
      maxToolIters: 1,
    });

    const toolContents: string[] = [];
    for await (const ev of loop.step("go")) {
      if (ev.role === "tool") toolContents.push(ev.content);
    }
    expect(toolContents[0]).toMatch(/\[child-augmenter-marker\]/);
    expect(toolContents[0]).not.toMatch(/\[budget:/);
  });
});
