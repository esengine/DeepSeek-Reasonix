import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WireEvent } from "../port/wire";
import { BACKGROUND_FLUSH_MS, createPacer } from "./deltas";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const ev = (kind: WireEvent["kind"], text = ""): WireEvent => ({ kind, text }) as WireEvent;

function pacer(shown: boolean) {
  const seen: WireEvent[] = [];
  const p = createPacer((e) => seen.push(e));
  p.show(shown);
  return { p, seen };
}

describe("a pane being looked at", () => {
  it("delivers every event the moment it arrives", () => {
    const { p, seen } = pacer(true);
    ["reasoning", "reasoning", "text", "text"].forEach((k) => p.push(ev(k as WireEvent["kind"])));
    expect(seen).toHaveLength(4);
  });
});

describe("a pane nobody is looking at", () => {
  it("holds the deltas that continue a stream and releases them together, in order", () => {
    const { p, seen } = pacer(false);
    p.push(ev("turn_started"));
    p.push(ev("text", "a"));
    expect(seen.map((e) => e.text)).toEqual(["", "a"]);
    for (const c of "bcdef") p.push(ev("text", c));
    expect(seen).toHaveLength(2);
    vi.advanceTimersByTime(BACKGROUND_FLUSH_MS - 1);
    expect(seen).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(seen.map((e) => e.text).join("")).toBe("abcdef");
  });

  it("delivers the first token of each stream at once so the thinking clock stops on time", () => {
    const { p, seen } = pacer(false);
    p.push(ev("turn_started"));
    p.push(ev("reasoning", "r1"));
    p.push(ev("reasoning", "r2"));
    expect(seen.map((e) => e.text)).toEqual(["", "r1"]);
    p.push(ev("text", "t1"));
    expect(seen.map((e) => e.text)).toEqual(["", "r1", "r2", "t1"]);
    p.push(ev("text", "t2"));
    expect(seen).toHaveLength(4);
  });

  it("delivers any other event at once, after whatever it was held behind", () => {
    const { p, seen } = pacer(false);
    p.push(ev("text", "a"));
    p.push(ev("text", "b"));
    p.push(ev("tool_dispatch"));
    expect(seen.map((e) => e.kind)).toEqual(["text", "text", "tool_dispatch"]);
    p.push(ev("text", "c"));
    p.push(ev("text", "d"));
    expect(seen.map((e) => e.text)).toEqual(["a", "b", "", "c"]);
  });

  it("releases everything held when it comes into view, without waiting for the timer", () => {
    const { p, seen } = pacer(false);
    p.push(ev("text", "a"));
    p.push(ev("text", "b"));
    p.push(ev("text", "c"));
    expect(seen).toHaveLength(1);
    p.show(true);
    expect(seen.map((e) => e.text)).toEqual(["a", "b", "c"]);
    vi.advanceTimersByTime(BACKGROUND_FLUSH_MS * 2);
    expect(seen).toHaveLength(3);
  });

  it("drops what it holds when the stream is rebuilt from the record", () => {
    const { p, seen } = pacer(false);
    p.push(ev("text", "a"));
    p.push(ev("text", "b"));
    p.drop();
    vi.advanceTimersByTime(BACKGROUND_FLUSH_MS * 2);
    expect(seen.map((e) => e.text)).toEqual(["a"]);
    p.push(ev("text", "c"));
    p.push(ev("text", "d"));
    expect(seen.map((e) => e.text)).toEqual(["a", "c"]);
  });
});
