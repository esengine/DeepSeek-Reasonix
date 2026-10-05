// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { Cache } from "./panels/Cache";
import { Context } from "./panels/Context";
import type { Metrics } from "../state/session";
import type { ContextBreakdown } from "../port/port";

afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

const metrics = (over: Partial<Metrics> = {}): Metrics =>
  ({ hit: 76_000, miss: 9_000, out: 0, bySource: {}, cost: 0, currency: "¥",
     prefixHash: "", prefixChanged: false, prefixReasons: [], bodyChanged: false,
     carriedMessages: 3, toolSchema: 0, estimated: false, alt: null, turn: 0, rounds: [],
     ...over } as Metrics);

const ctx = (used: number): ContextBreakdown =>
  ({ used, window: 1_000_000, compact_at: 160_000, boundary: "economic", capacity_at: 800_000,
     system: 20_000, tools: 12_000, user: 10_000, reply: 20_000, output: 20_000 });

// Every distinct rendering of a metric, in order. An update that eases from its
// old reading to its new one shows up here as the readings in between.
function watch(read: () => string) {
  const seen: string[] = [];
  const note = () => {
    const v = read();
    if (seen[seen.length - 1] !== v) seen.push(v);
  };
  note();
  return { seen, note };
}

// The contract these two share: a metric moves once per request, so one update
// is one displayed value. Interpolating between two of them puts numbers on
// screen that no request ever produced, and nothing on screen says which of
// them were measurements.
describe("one metric update is one displayed value", () => {
  it("moves the context usage figure in a single step", () => {
    vi.useFakeTimers();
    const view = render(<Context ctx={ctx(9_500)} legend />);
    const w = watch(() => view.container.querySelector(".ctxq")?.textContent ?? "");
    view.rerender(<Context ctx={ctx(11_400)} legend />);
    w.note();
    // Whatever a timer or a frame would have added lands here if anything does.
    for (let i = 0; i < 12; i++) {
      act(() => { vi.advanceTimersByTime(100); });
      w.note();
    }
    expect(w.seen.length).toBe(2);
    expect(w.seen[0]).toContain("9.5k");
    expect(w.seen[1]).not.toBe(w.seen[0]);
  });

  it("moves the cache ratio in a single step", () => {
    vi.useFakeTimers();
    const view = render(<Cache metrics={metrics()} />);
    const w = watch(() => view.container.querySelector(".lbl .c")?.textContent ?? "");
    view.rerender(<Cache metrics={metrics({ hit: 111_000, miss: 9_600 })} />);
    w.note();
    for (let i = 0; i < 12; i++) {
      act(() => { vi.advanceTimersByTime(100); });
      w.note();
    }
    expect(w.seen.length).toBe(2);
  });

  // The ratio is hit / (hit + miss). Easing it apart from the two numbers it is
  // derived from put a ratio on screen that did not match them.
  it("keeps the ratio agreeing with the two figures it comes from", () => {
    const { container } = render(<Cache metrics={metrics({ hit: 111_000, miss: 9_600 })} />);
    expect(container.querySelector(".lbl .c")?.textContent).toBe("92.0%");
    expect(container.querySelector(".nums")?.textContent).toContain("111k");
    expect(container.querySelector(".nums")?.textContent).toContain("9.6k");
  });
});

describe("what the cache block interrupts for", () => {
  it("says nothing at all before a request has been made", () => {
    expect(render(<Cache metrics={metrics({ hit: 0, miss: 0 })} />).container.textContent).toBe("");
  });

  // A cumulative ratio cannot signal a break: the longer a session runs, the
  // less one bad turn moves it. What can is the prefix having moved.
  it("is quiet while the prefix is holding", () => {
    const { container } = render(<Cache metrics={metrics()} />);
    expect(container.querySelector(".cachemoved")).toBeNull();
    expect(container.textContent).not.toMatch(/前缀变了|正文变了/);
  });

  it("surfaces the prefix moving, with the kernel's own reasons on it", () => {
    const { container } = render(
      <Cache metrics={metrics({ prefixChanged: true, prefixReasons: ["tools changed"] })} />,
    );
    const line = container.querySelector(".cachemoved");
    expect(line?.textContent).toContain("前缀变了");
    expect(line?.getAttribute("title")).toBe("tools changed");
  });

  it("surfaces a rewritten body too, which the hash alone cannot see", () => {
    const { container } = render(<Cache metrics={metrics({ bodyChanged: true })} />);
    expect(container.querySelector(".cachemoved")?.textContent).toContain("正文变了");
  });

  // The ratio is still there, as a figure beside the block's name rather than
  // as the largest number on the rail.
  it("keeps the ratio, without giving it the rail's biggest voice", () => {
    const { container } = render(<Cache metrics={metrics()} />);
    expect(container.querySelector(".lbl .c")?.textContent).toBe("89.4%");
    expect(container.querySelector(".big")).toBeNull();
  });
});
