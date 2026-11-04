// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { ToolCard } from "./ToolCard";
import type { StateTool } from "../../state/outputcap";

afterEach(() => { cleanup(); vi.useRealTimers(); });

const bash = (over: Partial<StateTool> = {}): StateTool => ({ id: "b1", name: "bash", args: '{"command":"go test ./..."}', readOnly: false, ...over });
const state = () => document.querySelector(".tool-state")!;

describe("a step in the activity group", () => {
  it("turns its spinner into a tick that pops, in the same place, only when it finishes under the reader", () => {
    const view = render(<ToolCard activity tool={bash()} running />);
    expect(state().querySelector(".step-spin")).not.toBeNull();
    view.rerender(<ToolCard activity tool={bash({ output: "ok", durationMs: 1200 })} running={false} />);
    expect(state().querySelector(".step-spin")).toBeNull();
    expect(state().hasAttribute("data-pop")).toBe(true);
    cleanup();
    render(<ToolCard activity tool={bash({ output: "ok" })} running={false} />);
    expect(state().hasAttribute("data-pop")).toBe(false);
  });

  it("shakes once when it fails under the reader, and not when it is drawn failed", () => {
    const view = render(<ToolCard activity tool={bash()} running />);
    view.rerender(<ToolCard activity tool={bash({ err: "exit status 1", execution: { exitCode: 1 } })} running={false} />);
    expect(document.querySelector(".call")!.hasAttribute("data-shake")).toBe(true);
    cleanup();
    render(<ToolCard activity tool={bash({ err: "exit status 1", execution: { exitCode: 1 } })} running={false} />);
    expect(document.querySelector(".call")!.hasAttribute("data-shake")).toBe(false);
  });

  it("says a failure in a box under its row and keeps the whole body folded until asked", async () => {
    render(<ToolCard activity tool={bash({ err: "exit status 1", output: "ok pkg/a\nFAIL TestRetryAfter", execution: { exitCode: 1 } })} running={false} />);
    expect(document.querySelector(".step-sum")!.textContent).toBe("退出 1");
    expect(document.querySelector(".step-err code")!.textContent).toBe("FAIL TestRetryAfter");
    expect(document.querySelector<HTMLDetailsElement>("details.tool-disclosure")!.open).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "看完整输出" }));
    expect(document.querySelector<HTMLDetailsElement>("details.tool-disclosure")!.open).toBe(true);
  });

  it("offers to stop a step that has run a while with nothing to show", () => {
    vi.useFakeTimers();
    const onStop = vi.fn();
    render(<ToolCard activity tool={bash()} running onStop={onStop} />);
    expect(document.querySelector(".step-wait")).toBeNull();
    act(() => void vi.advanceTimersByTime(11_000));
    expect(document.querySelector(".step-wait")!.textContent).toBe("还没有输出 · 停止这一条");
    expect(document.querySelector(".step-sum")!.textContent).toBe("11 秒");
    act(() => screen.getByRole("button", { name: "停止这一条" }).click());
    expect(onStop).toHaveBeenCalled();
  });

  it("counts a running step from when the kernel says it started, not from when its row was drawn", () => {
    vi.useFakeTimers();
    vi.setSystemTime(100_000);
    render(<ToolCard activity tool={bash({ startedAt: 88_000 })} running />);
    expect(document.querySelector(".step-sum")!.textContent).toBe("12 秒");
    act(() => void vi.advanceTimersByTime(1_000));
    expect(document.querySelector(".step-sum")!.textContent).toBe("13 秒");
  });
});
