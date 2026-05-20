import { describe, expect, it } from "vitest";
import { handleToolEvent } from "../src/cli/ui/hooks/handle-tool-event.js";
import type { Scrollback } from "../src/cli/ui/hooks/useScrollback.js";
import type { TurnTranslator } from "../src/cli/ui/state/TurnTranslator.js";
import type { LoopEvent } from "../src/loop.js";

interface Call {
  method: string;
  args: unknown[];
}

function makeLog(): { log: Scrollback; calls: Call[] } {
  const calls: Call[] = [];
  const record =
    <A extends unknown[], R>(method: string, value: R) =>
    (...args: A): R => {
      calls.push({ method, args });
      return value;
    };
  const log: Scrollback = {
    pushUser: record("pushUser", "user"),
    pushWarning: record("pushWarning", "warning"),
    pushError: record("pushError", "error"),
    pushInfo: record("pushInfo", "info"),
    pushTip: record("pushTip", "tip"),
    pushCtxPressureIfHigh: record("pushCtxPressureIfHigh", undefined),
    pushStepProgress: record("pushStepProgress", "step"),
    pushPlanAnnounce: record("pushPlanAnnounce", "plan"),
    showDoctor: record("showDoctor", "doctor"),
    showUsageVerbose: record("showUsageVerbose", "usage"),
    showPlan: record("showPlan", "plan"),
    completePlanStep: record("completePlanStep", undefined),
    showCtx: record("showCtx", "ctx"),
    startReasoning: record("startReasoning", "reasoning"),
    appendReasoning: record("appendReasoning", undefined),
    endReasoning: record("endReasoning", undefined),
    startStreaming: record("startStreaming", "streaming"),
    appendStreaming: record("appendStreaming", undefined),
    endStreaming: record("endStreaming", undefined),
    startTool: record("startTool", "tool"),
    appendToolOutput: record("appendToolOutput", undefined),
    endTool: record("endTool", undefined),
    retryTool: record("retryTool", undefined),
    thinking: record("thinking", "thinking"),
    abortTurn: record("abortTurn", undefined),
    endTurn: record("endTurn", undefined),
    reset: record("reset", undefined),
  };
  return { log, calls };
}

function makeEvent(content: unknown): LoopEvent {
  return {
    turn: 1,
    role: "tool",
    toolName: "mark_step_complete",
    content: JSON.stringify(content),
  };
}

describe("handleToolEvent", () => {
  it("logs a compact evidence summary for completed plan steps", () => {
    const { log, calls } = makeLog();
    const completions = new Map();

    handleToolEvent(
      makeEvent({
        kind: "step_completed",
        stepId: "step-1",
        result: "Updated lifecycle tests.",
        evidence: [
          {
            kind: "verification",
            summary: "lifecycle tests passed",
            command: "npm test -- tests/lifecycle.test.ts",
          },
          {
            kind: "diff",
            summary: "runtime transition added",
            paths: ["src/code/lifecycle.ts", "tests/lifecycle.test.ts"],
          },
        ],
      }),
      {
        flush: () => undefined,
        translator: { toolEnd: () => undefined } as unknown as TurnTranslator,
        setOngoingTool: () => undefined,
        setToolProgress: () => undefined,
        toolStartedAtRef: { current: null },
        setPendingShell: () => undefined,
        setPendingPlan: () => undefined,
        setPendingRevision: () => undefined,
        setPendingChoice: () => undefined,
        planStepsRef: { current: [{ id: "step-1", title: "Lifecycle", action: "Update tests" }] },
        completedStepIdsRef: { current: new Set<string>() },
        stepCompletionsRef: { current: completions },
        planBodyRef: { current: null },
        planSummaryRef: { current: null },
        persistPlanState: () => undefined,
        log,
        session: null,
        codeModeOn: true,
      },
    );

    expect(calls).toContainEqual({
      method: "pushInfo",
      args: [
        "evidence: verification - lifecycle tests passed (npm test -- tests/lifecycle.test.ts); diff - runtime transition added (src/code/lifecycle.ts, tests/lifecycle.test.ts)",
        "ghost",
      ],
    });
  });

  it("stores full host-side evidence when the model payload is compact", () => {
    const { log, calls } = makeLog();
    const completions = new Map();
    const pendingCompletions = new Map([
      [
        "step-1",
        {
          kind: "step_completed" as const,
          stepId: "step-1",
          result: "Updated lifecycle tests.",
          evidence: [
            {
              kind: "verification" as const,
              summary: "lifecycle tests passed",
              command: "npm test -- tests/lifecycle.test.ts",
            },
          ],
        },
      ],
    ]);

    handleToolEvent(
      makeEvent({
        kind: "step_completed",
        stepId: "step-1",
        result: "Updated lifecycle tests.",
        evidenceSummary: "verification: lifecycle tests passed",
      }),
      {
        flush: () => undefined,
        translator: { toolEnd: () => undefined } as unknown as TurnTranslator,
        setOngoingTool: () => undefined,
        setToolProgress: () => undefined,
        toolStartedAtRef: { current: null },
        setPendingShell: () => undefined,
        setPendingPlan: () => undefined,
        setPendingRevision: () => undefined,
        setPendingChoice: () => undefined,
        planStepsRef: { current: [{ id: "step-1", title: "Lifecycle", action: "Update tests" }] },
        completedStepIdsRef: { current: new Set<string>() },
        stepCompletionsRef: { current: completions },
        pendingStepCompletionsRef: { current: pendingCompletions },
        planBodyRef: { current: null },
        planSummaryRef: { current: null },
        persistPlanState: () => undefined,
        log,
        session: null,
        codeModeOn: true,
      },
    );

    expect(completions.get("step-1")?.evidence?.[0]).toMatchObject({
      command: "npm test -- tests/lifecycle.test.ts",
    });
    expect(pendingCompletions.has("step-1")).toBe(false);
    expect(calls).toContainEqual({
      method: "pushInfo",
      args: [
        "evidence: verification - lifecycle tests passed (npm test -- tests/lifecycle.test.ts)",
        "ghost",
      ],
    });
  });
});
