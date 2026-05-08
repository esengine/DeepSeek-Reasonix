import { describe, expect, it } from "vitest";
import { loopEventToDashboard } from "../src/cli/ui/effects/loop-to-dashboard.js";
import type { LoopEvent } from "../src/loop.js";

const ctx = { assistantId: "a-1" };

const ev = (overrides: Partial<LoopEvent>): LoopEvent => ({
  turn: 1,
  role: "status",
  content: "",
  ...overrides,
});

describe("loopEventToDashboard", () => {
  it("translates assistant_delta with content + reasoning", () => {
    const out = loopEventToDashboard(
      ev({ role: "assistant_delta", content: "hi", reasoningDelta: "think" }),
      ctx,
    );
    expect(out).toEqual({
      kind: "assistant_delta",
      id: "a-1",
      contentDelta: "hi",
      reasoningDelta: "think",
    });
  });

  it("emits undefined contentDelta when content is empty", () => {
    const out = loopEventToDashboard(
      ev({ role: "assistant_delta", content: "", reasoningDelta: "x" }),
      ctx,
    );
    expect(out).toMatchObject({ kind: "assistant_delta", contentDelta: undefined });
  });

  it("returns null for tool_start with no toolName", () => {
    const out = loopEventToDashboard(ev({ role: "tool_start" }), ctx);
    expect(out).toBeNull();
  });

  it("translates tool_start with toolName + args", () => {
    const out = loopEventToDashboard(
      ev({ role: "tool_start", toolName: "read_file", toolArgs: '{"path":"x"}' }),
      ctx,
    );
    expect(out).toMatchObject({
      kind: "tool_start",
      toolName: "read_file",
      args: '{"path":"x"}',
    });
  });

  it("translates tool result", () => {
    const out = loopEventToDashboard(
      ev({ role: "tool", toolName: "read_file", content: "lines...", toolArgs: "{}" }),
      ctx,
    );
    expect(out).toMatchObject({ kind: "tool", toolName: "read_file", content: "lines..." });
  });

  it("translates warning + error + status", () => {
    expect(loopEventToDashboard(ev({ role: "warning", content: "slow" }), ctx)).toMatchObject({
      kind: "warning",
      text: "slow",
    });
    expect(loopEventToDashboard(ev({ role: "error", content: "bad" }), ctx)).toMatchObject({
      kind: "error",
      text: "bad",
    });
    expect(loopEventToDashboard(ev({ role: "status", content: "thinking" }), ctx)).toMatchObject({
      kind: "status",
      text: "thinking",
    });
  });

  it("returns null for unrecognized roles", () => {
    expect(loopEventToDashboard(ev({ role: "assistant_final" }), ctx)).toBeNull();
    expect(loopEventToDashboard(ev({ role: "tool_call_delta" }), ctx)).toBeNull();
  });
});
