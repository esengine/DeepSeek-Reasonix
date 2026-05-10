import { describe, expect, it } from "vitest";
import { type SubagentActivity, reduceSubagentInnerEvent } from "../src/cli/ui/useSubagent.js";
import type { LoopEvent } from "../src/loop/types.js";
import type { SubagentEvent } from "../src/tools/subagent.js";

const baseActivity: SubagentActivity = {
  runId: "sub-1",
  startedAt: 0,
  task: "demo",
  iter: 0,
  elapsedMs: 0,
  phase: "exploring",
  lastInner: null,
};

function inner(
  runId: string,
  role: LoopEvent["role"],
  extra: Partial<LoopEvent> = {},
): SubagentEvent {
  return {
    kind: "inner",
    runId,
    task: "demo",
    inner: { turn: 1, role, content: "", ...extra },
  };
}

describe("reduceSubagentInnerEvent", () => {
  it("returns prev by reference for assistant_delta inner events", () => {
    const prev = [baseActivity];
    const next = reduceSubagentInnerEvent(
      prev,
      inner("sub-1", "assistant_delta", { content: "hi" }),
    );
    expect(next).toBe(prev);
  });

  it("returns prev by reference for reasoning deltas (no role -> pseudo) and other non-summarisable roles", () => {
    const prev = [baseActivity];
    expect(reduceSubagentInnerEvent(prev, inner("sub-1", "tool_call_delta"))).toBe(prev);
    expect(
      reduceSubagentInnerEvent(prev, inner("sub-1", "assistant_final", { content: "ok" })),
    ).toBe(prev);
    expect(reduceSubagentInnerEvent(prev, inner("sub-1", "done"))).toBe(prev);
  });

  it("returns prev by reference for inner events from unknown runs", () => {
    const prev = [baseActivity];
    const next = reduceSubagentInnerEvent(
      prev,
      inner("sub-999", "tool_start", { toolName: "read" }),
    );
    expect(next).toBe(prev);
  });

  it("updates only the matching row when an inner event summarises", () => {
    const a: SubagentActivity = { ...baseActivity, runId: "sub-1" };
    const b: SubagentActivity = { ...baseActivity, runId: "sub-2" };
    const prev = [a, b];
    const next = reduceSubagentInnerEvent(prev, inner("sub-2", "tool", { toolName: "grep" }));
    expect(next).not.toBe(prev);
    expect(next[0]).toBe(a);
    expect(next[1]).not.toBe(b);
    expect(next[1]?.lastInner?.label).toBe("grep");
    expect(next[1]?.lastInner?.meta).toBe("done");
  });

  it("returns prev by reference when progress repeats the same iter/elapsedMs", () => {
    const prev = [{ ...baseActivity, iter: 3, elapsedMs: 1000 }];
    const next = reduceSubagentInnerEvent(prev, {
      kind: "progress",
      runId: "sub-1",
      task: "demo",
      iter: 3,
      elapsedMs: 1000,
    });
    expect(next).toBe(prev);
  });

  it("updates the row when progress advances iter or elapsedMs", () => {
    const prev = [{ ...baseActivity, iter: 3, elapsedMs: 1000 }];
    const next = reduceSubagentInnerEvent(prev, {
      kind: "progress",
      runId: "sub-1",
      task: "demo",
      iter: 4,
      elapsedMs: 1500,
    });
    expect(next).not.toBe(prev);
    expect(next[0]?.iter).toBe(4);
    expect(next[0]?.elapsedMs).toBe(1500);
  });

  it("returns prev by reference when phase repeats", () => {
    const prev = [{ ...baseActivity, phase: "summarising" as const }];
    const next = reduceSubagentInnerEvent(prev, {
      kind: "phase",
      runId: "sub-1",
      task: "demo",
      phase: "summarising",
    });
    expect(next).toBe(prev);
  });

  it("updates the row on a phase transition", () => {
    const prev = [{ ...baseActivity, phase: "exploring" as const }];
    const next = reduceSubagentInnerEvent(prev, {
      kind: "phase",
      runId: "sub-1",
      task: "demo",
      phase: "summarising",
    });
    expect(next).not.toBe(prev);
    expect(next[0]?.phase).toBe("summarising");
  });

  it("returns prev unchanged for start/end events (those branches are handled outside the reducer)", () => {
    const prev = [baseActivity];
    expect(reduceSubagentInnerEvent(prev, { kind: "start", runId: "sub-2", task: "x" })).toBe(prev);
    expect(reduceSubagentInnerEvent(prev, { kind: "end", runId: "sub-1", task: "demo" })).toBe(
      prev,
    );
  });
});
