// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import "./testkit";
import { useRate } from "./num";
import { WINDOW_MS, sample, tokensPerSecond, type Sample } from "../port/tokens";

afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

// Every reading the hook produced, in order, deduplicated: what a reader would
// actually have watched the row do.
function drive() {
  let win: Sample[] = [];
  const seen: number[] = [];
  const h = renderHook(({ w }) => useRate(w, true), { initialProps: { w: win } });
  const read = () => {
    const v = Math.round(h.result.current);
    if (seen[seen.length - 1] !== v) seen.push(v);
  };
  read();
  return {
    seen,
    token(n = 40) {
      win = sample(win, n, Date.now());
      act(() => h.rerender({ w: win }));
      read();
    },
    // Stepped rather than jumped: a value that slides between two points has to
    // be caught, not sampled past.
    quiet(ms: number) {
      for (let i = 0; i < ms; i += 100) {
        act(() => {
          vi.advanceTimersByTime(100);
        });
        read();
      }
    },
  };
}

describe("throughput is an observation of what arrived", () => {
  // The defect this replaces, kept as a statement about the pure function rather
  // than a story about the caller: asked again and again with nothing new, it
  // answers a different number every time. Round 2 watched a caller do exactly
  // that on a 250ms clock — 17 consecutive readings, 87 → 2 tok/s, over 4.3s in
  // which the model produced nothing.
  it("re-asking the formula with no new tokens yields a descending series", () => {
    let win: Sample[] = [];
    for (const t of [0, 200, 400, 600, 800]) win = sample(win, 40, t);
    const asked: number[] = [];
    for (let now = 1000; now < 1000 + WINDOW_MS; now += 250) {
      asked.push(Math.round(tokensPerSecond(win, now)));
    }
    const falling = asked.filter((v, i) => i > 0 && v < asked[i - 1]).length;
    expect(falling).toBeGreaterThan(3);
    expect(new Set(asked).size).toBeGreaterThan(3);
  });

  // The invariant. Wall time may end a reading; it may not manufacture one.
  it("makes no new reading while nothing arrives", () => {
    vi.useFakeTimers();
    const d = drive();
    d.token();
    d.quiet(200);
    d.token();
    d.quiet(300);
    d.token();
    const settled = d.seen.length;
    expect(d.seen[settled - 1]).toBeGreaterThan(0);

    d.quiet(WINDOW_MS + 1000);
    // One transition, to nothing. Not a slide through values nobody produced.
    expect(d.seen.slice(settled)).toEqual([0]);
  });

  // The other half, or the fix above is satisfied by never changing at all.
  it("reads again as soon as tokens resume", () => {
    vi.useFakeTimers();
    const d = drive();
    d.token();
    d.quiet(200);
    d.token();
    d.quiet(WINDOW_MS + 500);
    expect(d.seen[d.seen.length - 1]).toBe(0);

    d.token();
    d.quiet(200);
    d.token();
    expect(d.seen[d.seen.length - 1]).toBeGreaterThan(0);
  });

  // It expires on the window it was measured over, not before: a reading that
  // vanished early would be the same defect pointed the other way.
  it("holds its last reading until the window closes", () => {
    vi.useFakeTimers();
    const d = drive();
    d.token();
    d.quiet(200);
    d.token();
    const held = d.seen[d.seen.length - 1];
    d.quiet(WINDOW_MS - 500);
    expect(d.seen[d.seen.length - 1]).toBe(held);
  });

  it("says nothing at all while no turn is running", () => {
    let win: Sample[] = [];
    for (const t of [0, 200, 400]) win = sample(win, 40, t);
    const h = renderHook(({ on }) => useRate(win, on), { initialProps: { on: false } });
    expect(h.result.current).toBe(0);
  });
});
