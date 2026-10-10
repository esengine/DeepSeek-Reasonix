// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import type { RuntimeView } from "../port/hub";
import { BACKGROUND_FLUSH_MS } from "./deltas";

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
    send: (ev: Partial<WireEvent>) => act(() => emit(ev as WireEvent)),
    answer: () => view.container.querySelector(".txt")?.textContent ?? "",
    show: (next: boolean) => act(() => void view.rerender(<Pane {...props} active={next} visible={next} />)),
    start: () => act(() => emit({ kind: "turn_started" } as WireEvent)),
    // Polls over one second of a running turn, counted from zero so a pane's
    // mount reads are excluded.
    pollsOverASecond: async () => {
      status.mockClear();
      await act(async () => void (await vi.advanceTimersByTimeAsync(1000)));
      return status.mock.calls.length;
    },
  };
}

describe("what a pane costs while nobody is looking at it", () => {
  // Every session stays mounted so a tab switch throws away no stream,
  // transcript or scroll position. That is affordable only while the off-screen
  // ones are quiet: an ungated poll costs each a round trip and a full
  // re-render, four times a second, scaling with the number of open panes.
  it("does not poll a running turn it is not showing", async () => {
    const hidden = open(false);
    hidden.start();
    expect(await hidden.pollsOverASecond()).toBe(0);
  });

  it("polls the turn it is showing", async () => {
    const shown = open(true);
    shown.start();
    expect(await shown.pollsOverASecond()).toBeGreaterThan(1);
  });

  // The task list is the kernel's and the transcript cannot answer for it:
  // /history alone carries the list as the model last wrote it, without the
  // complete_step advances since.
  it("asks the kernel for the task list rather than deriving it", () => {
    expect(open(true).todos).toHaveBeenCalled();
  });

  // A pane brought forward must not show pre-hide state for up to 250ms.
  it("reads once on the way back in", () => {
    const pane = open(false);
    pane.start();
    pane.status.mockClear();
    pane.show(true);
    expect(pane.status).toHaveBeenCalled();
  });
});

describe("what a pane draws while nobody is looking at it", () => {
  // The mount reads (history, status) and the lazily loaded renderer both
  // answer a tick later; the answer is read after they have.
  const settle = () => act(async () => void (await vi.advanceTimersByTimeAsync(50)));
  const opened = async (visible: boolean) => {
    const pane = open(visible);
    await settle();
    return pane;
  };
  const stream = async (pane: ReturnType<typeof open>, tail: string[]) => {
    pane.start();
    pane.send({ kind: "text", text: "a" });
    tail.forEach((text) => pane.send({ kind: "text", text }));
    await settle();
  };

  it("draws a streamed answer as it arrives when it is shown", async () => {
    const pane = await opened(true);
    await stream(pane, ["b", "c"]);
    expect(pane.answer()).toBe("abc");
  });

  it("takes the first token at once and the rest of the stream in one batch", async () => {
    const pane = await opened(false);
    await stream(pane, ["b", "c", "d"]);
    expect(pane.answer()).toBe("a");
    await act(async () => void (await vi.advanceTimersByTimeAsync(BACKGROUND_FLUSH_MS)));
    expect(pane.answer()).toBe("abcd");
  });

  it("catches up the moment it is brought forward", async () => {
    const pane = await opened(false);
    await stream(pane, ["b", "c"]);
    expect(pane.answer()).toBe("a");
    pane.show(true);
    await settle();
    expect(pane.answer()).toBe("abc");
  });

  it("does not hold an event that is not a delta", async () => {
    const pane = await opened(false);
    await stream(pane, ["b"]);
    pane.send({ kind: "message", text: "abc" });
    pane.send({ kind: "turn_done" });
    await settle();
    expect(pane.answer()).toBe("abc");
  });
});
