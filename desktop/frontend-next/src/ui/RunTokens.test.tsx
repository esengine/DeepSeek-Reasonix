// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { RunTokens } from "./RunTokens";
import { reduce, initialState } from "../state/session";
import type { SessionState } from "../state/session";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);

const draw = (p: { sent: number; received: number; estimated: boolean }) => render(<RunTokens {...p} />).container;
const feed = (s: SessionState, ...evs: WireEvent[]) => evs.reduce(reduce, s);

// The number climbs while the model writes and settles when the round bills.
// Both are shown in the same place, so which one is on screen has to be said.
describe("the run's token readings", () => {
  it("says nothing before a turn has moved either number", () => {
    expect(draw({ sent: 0, received: 0, estimated: false }).querySelector(".studio-runtokens")).toBeNull();
  });

  it("marks a live reading as an estimate rather than passing it off as billed", () => {
    const live = draw({ sent: 1200, received: 48, estimated: true });
    expect(live.querySelector('[data-io="down"]')?.hasAttribute("data-est")).toBe(true);
    expect(live.querySelector("small")?.textContent).toContain("≈");
    const billed = draw({ sent: 1200, received: 96, estimated: false });
    expect(billed.querySelector('[data-io="down"]')?.hasAttribute("data-est")).toBe(false);
    expect(billed.querySelector("small")?.textContent).toBe("tokens");
  });

  it("lights a number that just moved, and lets it go again", () => {
    vi.useFakeTimers();
    try {
      const view = render(<RunTokens sent={10} received={0} estimated={false} />);
      view.rerender(<RunTokens sent={10} received={40} estimated />);
      expect(view.container.querySelector('[data-io="down"]')?.hasAttribute("data-landed")).toBe(true);
      // Dropped on a timer, so a stream that stops does not leave it lit.
      act(() => { vi.advanceTimersByTime(600); });
      expect(view.container.querySelector('[data-io="down"]')?.hasAttribute("data-landed")).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
});

// The reading has to come from somewhere the reducer owns, and stop being an
// estimate the moment the kernel says what the round actually cost.
describe("the estimate behind it", () => {
  it("climbs on streamed text and is replaced by what the round billed", () => {
    const streaming = feed(initialState, { kind: "turn_started" } as WireEvent, { kind: "text", text: "hello there" } as WireEvent);
    expect(streaming.outLive).toBeGreaterThan(0);
    expect(streaming.metrics.out).toBe(0);

    const billed = feed(streaming, {
      kind: "usage",
      usage: { cacheHitTokens: 0, cacheMissTokens: 0, completionTokens: 7 },
    } as WireEvent);
    expect(billed.outLive).toBe(0);
    expect(billed.metrics.out).toBe(7);
  });

  it("starts each turn from nothing so one turn's estimate never rides the next", () => {
    const first = feed(initialState, { kind: "turn_started" } as WireEvent, { kind: "text", text: "abc" } as WireEvent);
    expect(feed(first, { kind: "turn_started" } as WireEvent).outLive).toBe(0);
  });
});
