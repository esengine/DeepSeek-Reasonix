// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "../testkit";
import { SayCard } from "./SayCard";
import { PaneShown } from "../shown";
import { appendText } from "../../state/say";
import type { Item } from "../../state/session";

const NOW = 1_700_000_000_000;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  window.matchMedia ??= (() => ({ matches: true, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

type Say = Extract<Item, { t: "say" }>;
const thinking = () => appendText([], "考虑中", "reasoning")[0] as Say;
const clock = (c: HTMLElement) => c.querySelector(".fold")?.textContent;

function draw(item: Say, shown: boolean) {
  const view = render(<PaneShown.Provider value={shown}><SayCard item={item} /></PaneShown.Provider>);
  return {
    view,
    show: (next: boolean) => view.rerender(<PaneShown.Provider value={next}><SayCard item={item} /></PaneShown.Provider>),
    text: () => clock(view.container),
  };
}

describe("the thinking clock", () => {
  it("runs on a card being looked at", () => {
    const card = draw(thinking(), true);
    expect(card.text()).toBe("思考中 0.0 秒");
    act(() => void vi.advanceTimersByTime(1500));
    expect(card.text()).toBe("思考中 1.5 秒");
  });

  it("sets no timer for a card in a pane nobody is looking at", () => {
    const tick = vi.spyOn(window, "setInterval");
    const card = draw(thinking(), false);
    act(() => void vi.advanceTimersByTime(3000));
    expect(tick).not.toHaveBeenCalled();
    expect(card.text()).toBe("思考中 0.0 秒");
  });

  it("reads the true elapsed time the moment the pane is brought forward", () => {
    const card = draw(thinking(), false);
    act(() => void vi.advanceTimersByTime(3000));
    act(() => card.show(true));
    expect(card.text()).toBe("思考中 3.0 秒");
    act(() => void vi.advanceTimersByTime(500));
    expect(card.text()).toBe("思考中 3.5 秒");
  });

  it("stops again when the pane goes behind another", () => {
    const card = draw(thinking(), true);
    act(() => void vi.advanceTimersByTime(1000));
    act(() => card.show(false));
    act(() => void vi.advanceTimersByTime(4000));
    expect(card.text()).toBe("思考中 1.0 秒");
  });
});

describe("the folded thought's length", () => {
  const done = (text: string, reasoning: string): Say => ({ t: "say", id: "d", text, reasoning, done: true, thoughtMs: 2300 });

  it("reads in graphemes and keeps the elapsed time", () => {
    const card = draw(done("答", "想🙂想"), true);
    expect(card.text()).toBe("思考 2.3 秒 · 3 字");
  });

  it("is not counted again while the answer beneath it streams", () => {
    const reasoning = "分析".repeat(40);
    const expanded = vi.fn();
    const iterator = String.prototype[Symbol.iterator];
    vi.spyOn(String.prototype, Symbol.iterator).mockImplementation(function (this: string) {
      if (String(this) === reasoning) expanded();
      return iterator.call(this);
    });
    const card = draw(done("答", reasoning), true);
    const first = expanded.mock.calls.length;
    expect(first).toBeGreaterThan(0);
    for (const text of ["答一", "答一二", "答一二三"]) {
      card.view.rerender(<PaneShown.Provider value={true}><SayCard item={done(text, reasoning)} /></PaneShown.Provider>);
    }
    expect(expanded.mock.calls.length).toBe(first);
    expect(card.text()).toBe("思考 2.3 秒 · 80 字");
  });
});
