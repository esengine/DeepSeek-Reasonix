// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { useStatusPoll } from "./useStatusPoll";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import type { RuntimeView } from "../port/hub";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));

const rt = { id: "p1", root: "/w", name: "w" } as RuntimeView;

// A pane on a port whose reads are counted, keeping the subscription so a turn
// can be started the way the kernel starts one.
function open(visible: boolean) {
  const port = new MockPort();
  let emit: (ev: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const status = vi.spyOn(port, "status");
  const todos = vi.spyOn(port, "todos");
  const props = {
    port: port as AgentPort,
    rt,
    title: "w",
    active: visible,
    visible,
    sideHost: null,
    side: false,
    onFocus: () => {},
    onReport: () => {},
    onSessionChanged: () => {},
    pulse: 0,
    findPulse: 0,
    onSettings: () => {},
    needsProject: false,
    onOpenProject: () => {},
    onKeepHere: () => {},
    theme: "dark",
    dockW: 560,
    dockMax: 880,
    onDockW: () => {},
  };
  const view = render(<Pane {...props} />);
  return {
    status,
    todos,
    show: (next: boolean) => act(() => void view.rerender(<Pane {...props} active={next} visible={next} />)),
    start: () => act(() => emit({ kind: "turn_started" } as WireEvent)),
  };
}


describe("a slow /status must not pile up requests", () => {
  it("keeps at most one /status read in flight while the kernel is slow", () => {
    const p = open(true);
    p.status.mockImplementation(() => new Promise(() => {}));
    p.start();
    p.status.mockClear();
    act(() => void vi.advanceTimersByTime(5000));
    expect(p.status.mock.calls.length).toBeLessThanOrEqual(1);
  });

  it("keeps polling at the steady cadence once reads come back", async () => {
    const p = open(true);
    p.start();
    await act(async () => void (await vi.advanceTimersByTimeAsync(300)));
    p.status.mockClear();
    await act(async () => void (await vi.advanceTimersByTimeAsync(1000)));
    expect(p.status.mock.calls.length).toBeGreaterThanOrEqual(3);
  });

  it("schedules the next read only after the previous one settles", async () => {
    const p = open(true);
    const releases: (() => void)[] = [];
    p.status.mockImplementation(() => new Promise((resolve) => releases.push(() => resolve({} as never))));
    p.start();
    const before = p.status.mock.calls.length;
    await act(async () => void (await vi.advanceTimersByTimeAsync(2000)));
    expect(p.status.mock.calls.length).toBe(before);
    await act(async () => {
      releases.splice(0).forEach((r) => r());
      await vi.advanceTimersByTimeAsync(400);
    });
    expect(p.status.mock.calls.length).toBe(before + 1);
  });

  it("keeps polling after a read fails", async () => {
    const p = open(true);
    p.status.mockRejectedValue(new Error("boom"));
    p.start();
    p.status.mockClear();
    await act(async () => void (await vi.advanceTimersByTimeAsync(1000)));
    expect(p.status.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("keeps polling when a read rejects", async () => {
    const read = vi.fn().mockRejectedValueOnce(new Error("bad status")).mockResolvedValue(undefined);
    renderHook(() => useStatusPoll(true, read));
    await act(async () => void (await vi.advanceTimersByTimeAsync(1000)));
    expect(read.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("stops reading when the turn ends", async () => {
    const p = open(true);
    p.start();
    await act(async () => void (await vi.advanceTimersByTimeAsync(300)));
    p.show(false);
    p.status.mockClear();
    await act(async () => void (await vi.advanceTimersByTimeAsync(2000)));
    expect(p.status.mock.calls.length).toBe(0);
  });
});
