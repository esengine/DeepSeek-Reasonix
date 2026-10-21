// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { MAX_LOOP_INTERVAL_MS, MIN_LOOP_INTERVAL_MS, loopLabel, parseLoopCommand, parseLoopInterval, useLoopMode } from "./loop_mode";

afterEach(() => {
  vi.useRealTimers();
});

describe("/loop interval", () => {
  it("reads seconds, minutes and hours, with or without a suffix", () => {
    expect(parseLoopInterval("30s")).toBe(30_000);
    expect(parseLoopInterval("30")).toBe(30_000);
    expect(parseLoopInterval("5m")).toBe(300_000);
    expect(parseLoopInterval("1h")).toBe(3_600_000);
    expect(parseLoopInterval("90sec")).toBe(90_000);
  });

  it("refuses what a timer cannot honour", () => {
    expect(parseLoopInterval("4s")).toBeNull();
    expect(parseLoopInterval(`${MIN_LOOP_INTERVAL_MS - 1}ms`)).toBeNull();
    expect(parseLoopInterval("7h")).toBeNull();
    expect(parseLoopInterval("soon")).toBeNull();
    expect(parseLoopInterval("0s")).toBeNull();
    expect(parseLoopInterval(`${MAX_LOOP_INTERVAL_MS + 1000}s`)).toBeNull();
  });
});

describe("/loop command", () => {
  it("is not a loop line at all when the word is not the command", () => {
    expect(parseLoopCommand("loops are useful")).toBeNull();
    expect(parseLoopCommand("please /loop")).toBeNull();
  });

  it("starts, stops and reports", () => {
    expect(parseLoopCommand("/loop 30s check the build")).toEqual({ kind: "start", intervalMs: 30_000, prompt: "check the build" });
    expect(parseLoopCommand("/loop stop")).toEqual({ kind: "stop" });
    expect(parseLoopCommand("/loop")).toEqual({ kind: "status" });
  });

  it("separates a bad interval from a missing prompt", () => {
    expect(parseLoopCommand("/loop nope do it")).toEqual({ kind: "error", reason: "usage" });
    expect(parseLoopCommand("/loop 30s")).toEqual({ kind: "error", reason: "no-prompt" });
  });
});

describe("a running loop", () => {
  it("fires the prompt on its interval and counts the firings", async () => {
    vi.useFakeTimers();
    const fire = vi.fn(async () => true);
    const { result } = renderHook(() => useLoopMode({ fire, busy: false }));

    act(() => {
      result.current.command("/loop 10s check the build");
    });
    expect(fire).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fire).toHaveBeenCalledWith("check the build");
    expect(result.current.loop?.iter).toBe(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fire).toHaveBeenCalledTimes(2);
    expect(result.current.loop?.iter).toBe(2);
  });

  it("waits instead of stacking turns when one is already running", async () => {
    vi.useFakeTimers();
    const fire = vi.fn(async () => true);
    const { result, rerender } = renderHook(({ busy }) => useLoopMode({ fire, busy }), { initialProps: { busy: false } });

    act(() => {
      result.current.command("/loop 10s check the build");
    });
    rerender({ busy: true });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fire).not.toHaveBeenCalled();
    expect(result.current.loop?.iter).toBe(0);

    rerender({ busy: false });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(fire).toHaveBeenCalledWith("check the build");
  });

  it("stops when asked, and stops arming timers", async () => {
    vi.useFakeTimers();
    const fire = vi.fn(async () => true);
    const { result } = renderHook(() => useLoopMode({ fire, busy: false }));

    act(() => {
      result.current.command("/loop 10s check the build");
    });
    let stopped: ReturnType<typeof result.current.command> | undefined;
    act(() => {
      stopped = result.current.command("/loop stop");
    });
    expect(stopped).toEqual({ handled: true, reply: "已停止循环（共 0 次）" });
    expect(result.current.loop).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(fire).not.toHaveBeenCalled();
  });

  it("stops rather than repeating a submit the kernel keeps refusing", async () => {
    vi.useFakeTimers();
    const fire = vi.fn(async () => {
      throw new Error("busy");
    });
    const { result } = renderHook(() => useLoopMode({ fire, busy: false }));

    act(() => {
      result.current.command("/loop 10s check the build");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fire).toHaveBeenCalledTimes(1);
    expect(result.current.loop).toBeNull();
  });

  it("leaves a line that is not a loop command to the ordinary submit", () => {
    const fire = vi.fn(async () => true);
    const { result } = renderHook(() => useLoopMode({ fire, busy: false }));
    let answered: ReturnType<typeof result.current.command> | undefined;
    act(() => {
      answered = result.current.command("what does this file do?");
    });
    expect(answered).toEqual({ handled: false });
  });

  it("names the cadence and the count on the line above the composer", () => {
    const line = loopLabel({ prompt: "check the build", intervalMs: 300_000, dueAt: 0, iter: 3 });
    expect(line).toContain("5m");
    expect(line).toContain("3");
    expect(line).toContain("check the build");
  });
});
