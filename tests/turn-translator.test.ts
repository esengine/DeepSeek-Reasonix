import { describe, expect, it } from "vitest";
import type { Scrollback } from "../src/cli/ui/hooks/useScrollback.js";
import { TurnTranslator } from "../src/cli/ui/state/TurnTranslator.js";
import { Usage } from "../src/client.js";
import type { TurnStats } from "../src/telemetry/stats.js";

interface Call {
  method: string;
  args: unknown[];
}

function makeMockLog(): { log: Scrollback; calls: Call[] } {
  const calls: Call[] = [];
  let n = 0;
  const next = (prefix: string) => {
    n += 1;
    return `${prefix}-${n}`;
  };
  const record =
    <A extends unknown[], R>(method: string, returnValue: (...args: A) => R) =>
    (...args: A): R => {
      calls.push({ method, args });
      return returnValue(...args);
    };
  const log: Scrollback = {
    pushUser: record("pushUser", () => next("u")),
    pushWarning: record("pushWarning", () => next("warn")),
    pushError: record("pushError", () => next("err")),
    pushInfo: record("pushInfo", () => next("info")),
    pushStepProgress: record("pushStepProgress", () => next("step")),
    pushPlanAnnounce: record("pushPlanAnnounce", () => next("plan")),
    startReasoning: record("startReasoning", () => next("r")),
    appendReasoning: record("appendReasoning", () => undefined),
    endReasoning: record("endReasoning", () => undefined),
    startStreaming: record("startStreaming", () => next("s")),
    appendStreaming: record("appendStreaming", () => undefined),
    endStreaming: record("endStreaming", () => undefined),
    startTool: record("startTool", () => next("tool")),
    appendToolOutput: record("appendToolOutput", () => undefined),
    endTool: record("endTool", () => undefined),
    retryTool: record("retryTool", () => undefined),
    thinking: record("thinking", () => next("think")),
    abortTurn: record("abortTurn", () => undefined),
    endTurn: record("endTurn", () => undefined),
  };
  return { log, calls };
}

const stats = (overrides: Partial<TurnStats> = {}): TurnStats => ({
  turn: 1,
  model: "test",
  usage: new Usage(1000, 50, 1050, 800, 200),
  cost: 0.0014,
  cacheHitRatio: 0.91,
  ...overrides,
});

describe("TurnTranslator", () => {
  it("starts reasoning card lazily on first chunk and appends thereafter", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.flushBuffers("first", "");
    t.flushBuffers("second", "");
    const methods = calls.map((c) => c.method);
    expect(methods).toEqual(["startReasoning", "appendReasoning", "appendReasoning"]);
    expect(calls[1]?.args[1]).toBe("first");
    expect(calls[2]?.args[1]).toBe("second");
  });

  it("starts streaming and reasoning cards independently", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.flushBuffers("", "hello ");
    t.flushBuffers("brain", "world");
    const methods = calls.map((c) => c.method);
    expect(methods).toEqual([
      "startStreaming",
      "appendStreaming",
      "startReasoning",
      "appendReasoning",
      "appendStreaming",
    ]);
  });

  it("forwards an explicit model id into start{Reasoning,Streaming} so /pro turns get the right badge (#403)", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.flushBuffers("first reasoning", "first stream", "deepseek-v4-pro");
    const startReasoning = calls.find((c) => c.method === "startReasoning");
    const startStreaming = calls.find((c) => c.method === "startStreaming");
    expect(startReasoning?.args[0]).toBe("deepseek-v4-pro");
    expect(startStreaming?.args[0]).toBe("deepseek-v4-pro");
  });

  it("toolStart + toolEnd pair sends startTool then endTool with elapsedMs", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.toolStart("read_file", { path: "src/x.ts" });
    t.toolEnd("ok 250 lines");
    const startCall = calls.find((c) => c.method === "startTool");
    const endCall = calls.find((c) => c.method === "endTool");
    expect(startCall?.args).toEqual(["read_file", { path: "src/x.ts" }]);
    expect(endCall?.args[0]).toBe("tool-1");
    const endInfo = endCall?.args[1] as { output: string; elapsedMs: number };
    expect(endInfo.output).toBe("ok 250 lines");
    expect(endInfo.elapsedMs).toBeGreaterThanOrEqual(0);
  });

  it("toolEnd is a no-op when no tool was started", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.toolEnd("stray output");
    expect(calls).toEqual([]);
  });

  it("reasoningDone derives paragraphs and tokens from accumulated text", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    const sample = "Two paragraphs.\n\nLine break here.";
    t.flushBuffers("hello", "");
    t.reasoningDone(sample);
    const endCall = calls.find((c) => c.method === "endReasoning");
    expect(endCall?.args[1]).toBe(2);
    expect(endCall?.args[2]).toBe(Math.round(sample.length / 4));
  });

  it("reasoningDone is a no-op when no reasoning card was opened", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.reasoningDone("orphaned");
    expect(calls).toEqual([]);
  });

  it("turnEnd dispatches a normalized usage payload", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.turnEnd(stats(), "abc");
    const endTurn = calls.find((c) => c.method === "endTurn");
    expect(endTurn?.args[0]).toEqual({
      prompt: 1000,
      reason: 1,
      output: 50,
      cacheHit: 0.91,
      cost: 0.0014,
    });
  });

  it("abort closes any open reasoning, streaming, and tool cards", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.flushBuffers("partial", "stream");
    t.toolStart("run_command", { cmd: "ls" });
    t.abort();
    const methods = calls.map((c) => c.method);
    expect(methods).toContain("endReasoning");
    expect(methods).toContain("endStreaming");
    expect(methods).toContain("endTool");
    expect(methods).toContain("abortTurn");
  });

  it("retryTool annotates an open tool card", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.toolStart("npm", "test");
    t.toolRetry(2, 3);
    const retry = calls.find((c) => c.method === "retryTool");
    expect(retry?.args).toEqual(["tool-1", 2, 3]);
  });

  it("retryTool is a no-op when no tool is open", () => {
    const { log, calls } = makeMockLog();
    const t = new TurnTranslator(log);
    t.toolRetry(1, 3);
    const retry = calls.find((c) => c.method === "retryTool");
    expect(retry).toBeUndefined();
  });
});
