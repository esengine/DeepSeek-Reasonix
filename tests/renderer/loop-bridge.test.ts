import { describe, expect, it } from "vitest";
import { makeLoopBridge } from "../../src/cli/ui/loop-bridge.js";
import type { AgentEvent } from "../../src/cli/ui/state/events.js";
import { Usage } from "../../src/client.js";
import type { LoopEvent } from "../../src/loop.js";

function ev(partial: Partial<LoopEvent> & { role: LoopEvent["role"] }): LoopEvent {
  return { turn: 1, content: "", ...partial };
}

function types(events: ReadonlyArray<AgentEvent>): ReadonlyArray<string> {
  return events.map((e) => e.type);
}

describe("makeLoopBridge — basic chat", () => {
  it("first reasoning delta opens turn + reasoning card", () => {
    const bridge = makeLoopBridge("t-1");
    const out = bridge.consume(ev({ role: "assistant_delta", reasoningDelta: "hmm" }));
    expect(types(out)).toEqual(["turn.start", "reasoning.start", "reasoning.chunk"]);
  });

  it("second reasoning delta only emits a chunk", () => {
    const bridge = makeLoopBridge("t-2");
    bridge.consume(ev({ role: "assistant_delta", reasoningDelta: "first" }));
    const out = bridge.consume(ev({ role: "assistant_delta", reasoningDelta: "second" }));
    expect(types(out)).toEqual(["reasoning.chunk"]);
  });

  it("first content delta closes reasoning + opens streaming", () => {
    const bridge = makeLoopBridge("t-3");
    bridge.consume(ev({ role: "assistant_delta", reasoningDelta: "thought" }));
    const out = bridge.consume(ev({ role: "assistant_delta", content: "answer " }));
    expect(types(out)).toEqual(["reasoning.end", "streaming.start", "streaming.chunk"]);
  });

  it("done closes any open cards + emits turn.end", () => {
    const bridge = makeLoopBridge("t-4");
    bridge.consume(ev({ role: "assistant_delta", content: "hello" }));
    const out = bridge.consume(ev({ role: "done" }));
    expect(types(out)).toEqual(["streaming.end", "turn.end"]);
  });
});

describe("makeLoopBridge — tools", () => {
  it("tool_start emits tool.start (and settles streaming if open)", () => {
    const bridge = makeLoopBridge("t-5");
    bridge.consume(ev({ role: "assistant_delta", content: "calling tool" }));
    const out = bridge.consume(
      ev({ role: "tool_start", toolName: "shell", toolArgs: '{"cmd":"ls"}' }),
    );
    expect(types(out)).toEqual(["streaming.end", "tool.start"]);
    const ts = out.find((e) => e.type === "tool.start");
    expect(ts && (ts.type === "tool.start" ? ts.name : "")).toBe("shell");
  });

  it("tool after tool_start emits tool.end with output", () => {
    const bridge = makeLoopBridge("t-6");
    bridge.consume(ev({ role: "tool_start", toolName: "shell", toolArgs: '{"cmd":"ls"}' }));
    const out = bridge.consume(ev({ role: "tool", toolName: "shell", content: "src/\n" }));
    expect(types(out)).toEqual(["tool.end"]);
    const te = out[0];
    if (te?.type !== "tool.end") throw new Error("expected tool.end");
    expect(te.output).toBe("src/\n");
  });

  it("bare tool with no preceding tool_start fabricates a start+end pair", () => {
    const bridge = makeLoopBridge("t-7");
    const out = bridge.consume(ev({ role: "tool", toolName: "harvest", content: "ok" }));
    expect(types(out)).toEqual(["turn.start", "tool.start", "tool.end"]);
  });

  it("multiple tools allocate distinct ids", () => {
    const bridge = makeLoopBridge("t-8");
    const a = bridge.consume(ev({ role: "tool_start", toolName: "shell" }));
    bridge.consume(ev({ role: "tool", toolName: "shell", content: "ok" }));
    const b = bridge.consume(ev({ role: "tool_start", toolName: "read_file" }));
    const idA = a.find((e) => e.type === "tool.start");
    const idB = b.find((e) => e.type === "tool.start");
    if (idA?.type !== "tool.start" || idB?.type !== "tool.start")
      throw new Error("missing tool.start");
    expect(idA.id).not.toBe(idB.id);
  });
});

describe("makeLoopBridge — errors + abort", () => {
  it("error closes any open card with aborted=true and fires turn.abort", () => {
    const bridge = makeLoopBridge("t-9");
    bridge.consume(ev({ role: "assistant_delta", content: "partial" }));
    const out = bridge.consume(ev({ role: "error", error: "network blew up" }));
    expect(types(out)).toEqual(["streaming.end", "turn.abort"]);
    const se = out.find((e) => e.type === "streaming.end");
    expect(se && (se.type === "streaming.end" ? se.aborted : false)).toBe(true);
  });
});

describe("makeLoopBridge — usage rolls into turn.end", () => {
  it("done with stats populates the usage payload", () => {
    const bridge = makeLoopBridge("t-10");
    bridge.consume(ev({ role: "assistant_delta", content: "ok" }));
    const usage = new Usage(120, 30, 150, 80, 40);
    const out = bridge.consume(
      ev({
        role: "done",
        stats: { turn: 1, model: "deepseek-chat", usage, cost: 0.0042, cacheHitRatio: 0.66 },
      }),
    );
    const end = out.find((e) => e.type === "turn.end");
    if (end?.type !== "turn.end") throw new Error("expected turn.end");
    expect(end.usage.prompt).toBe(120);
    expect(end.usage.output).toBe(30);
    expect(end.usage.cost).toBeCloseTo(0.0042, 5);
  });
});

describe("makeLoopBridge — non-primary roles are no-ops", () => {
  it("status / tool_call_delta / warning / branch_* drop silently", () => {
    const bridge = makeLoopBridge("t-11");
    expect(bridge.consume(ev({ role: "status", content: "thinking…" }))).toEqual([]);
    expect(bridge.consume(ev({ role: "tool_call_delta", toolName: "shell" }))).toEqual([]);
    expect(bridge.consume(ev({ role: "warning", content: "rate limit warning" }))).toEqual([]);
  });
});
