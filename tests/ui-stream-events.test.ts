import type { SetStateAction } from "react";
import { describe, expect, it, vi } from "vitest";
import { handleErrorEvent, handleToolStart } from "../src/cli/ui/hooks/handle-stream-events.js";
import type { Scrollback } from "../src/cli/ui/hooks/useScrollback.js";
import type { TurnTranslator } from "../src/cli/ui/state/TurnTranslator.js";
import type { LoopEvent } from "../src/loop.js";

type OngoingTool = { name: string; args?: string } | null;
type ToolProgress = { progress: number; total?: number; message?: string } | null;

function applyState<T>(current: T, next: SetStateAction<T>): T {
  return typeof next === "function" ? (next as (prev: T) => T)(current) : next;
}

describe("stream event handlers", () => {
  it("clears the ongoing tool row when an error interrupts a tool", () => {
    let ongoingTool: OngoingTool = null;
    let toolProgress: ToolProgress = { progress: 1, message: "working" };
    const toolStartedAtRef = { current: null as number | null };
    const setOngoingTool = vi.fn((next: SetStateAction<OngoingTool>) => {
      ongoingTool = applyState(ongoingTool, next);
    });
    const setToolProgress = vi.fn((next: SetStateAction<ToolProgress>) => {
      toolProgress = applyState(toolProgress, next);
    });
    const translator = {
      toolStart: vi.fn(),
      toolAbort: vi.fn(),
    } as unknown as TurnTranslator;
    const log = {
      pushError: vi.fn(() => "err-1"),
    } as unknown as Scrollback;

    handleToolStart(
      {
        turn: 1,
        role: "tool_start",
        content: "",
        toolName: "read_file",
        toolArgs: '{"path":"src/x.ts"}',
      } satisfies LoopEvent,
      {
        setOngoingTool,
        setToolProgress,
        toolStartedAtRef,
        translator,
        codeModeOn: false,
        recordRecentFile: vi.fn(),
      },
    );

    expect(ongoingTool).toEqual({ name: "read_file", args: '{"path":"src/x.ts"}' });
    expect(toolStartedAtRef.current).not.toBeNull();

    handleErrorEvent(
      {
        turn: 1,
        role: "error",
        content: "",
        error: "Error: tool crashed",
      } satisfies LoopEvent,
      {
        log,
        setOngoingTool,
        setToolProgress,
        toolStartedAtRef,
        translator,
      },
    );

    expect(ongoingTool).toBeNull();
    expect(toolProgress).toBeNull();
    expect(toolStartedAtRef.current).toBeNull();
    expect(translator.toolAbort).toHaveBeenCalledWith("Error: tool crashed");
    expect(log.pushError).toHaveBeenCalledWith("Error", "Error: tool crashed");
  });
});
