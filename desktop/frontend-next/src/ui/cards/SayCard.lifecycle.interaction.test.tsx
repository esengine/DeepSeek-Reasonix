// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "../testkit";
import { SayCard } from "./SayCard";
import { PaneShown } from "../shown";
import { initialState, reduce, type Item, type SessionEvent } from "../../state/session";

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(1_700_000_000_000); });
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });

const ev = (kind: string, fields: Record<string, unknown> = {}) => ({ kind, ...fields }) as SessionEvent;
const says = (items: Item[]) => items.filter((item): item is Extract<Item, { t: "say" }> => item.t === "say");
const labels = () => [...document.querySelectorAll(".think-label:not(.think-out)")].map((node) => node.textContent);
const cards = (items: Item[]) => <PaneShown.Provider value={true}>{says(items).map((item) => <SayCard key={item.id} item={item} />)}</PaneShown.Provider>;

it("keeps a completed thought still while the call and later rounds continue", () => {
  let state = reduce(reduce(initialState, ev("turn_started")), ev("reasoning", { text: "先检查" }));
  const tick = vi.spyOn(window, "setInterval");
  const view = render(cards(state.items));
  act(() => void vi.advanceTimersByTime(1200));
  expect(labels()).toEqual(["思考中 · 1.2 秒"]);
  state = reduce(state, ev("tool_dispatch", { tool: { id: "call", name: "bash", partial: true } }));
  view.rerender(cards(state.items));
  expect(labels()).toEqual(["思考 1.2 秒 · 3 字"]);
  const timers = tick.mock.calls.length;
  act(() => void vi.advanceTimersByTime(1_800_000));
  expect(labels()).toEqual(["思考 1.2 秒 · 3 字"]);
  expect(tick).toHaveBeenCalledTimes(timers);
  state = reduce(state, ev("text", { text: "开始检查。" }));
  state = reduce(state, ev("message", { text: "开始检查。", reasoning: "先检查", thoughtMs: 1000 }));
  state = reduce(state, ev("tool_result", { tool: { id: "call", name: "bash", output: "done" } }));
  state = reduce(state, ev("reasoning", { text: "再修改" }));
  view.rerender(cards(state.items));
  act(() => void vi.advanceTimersByTime(500));
  expect(labels()).toEqual(["思考 1.0 秒 · 3 字", "思考中 · 0.5 秒"]);
  state = reduce(state, ev("turn_done"));
  view.rerender(cards(state.items));
  act(() => void vi.advanceTimersByTime(1_800_000));
  expect(labels()).toEqual(["思考 1.0 秒 · 3 字", "思考 0.5 秒 · 3 字"]);
  expect(document.querySelector(".think[data-live]")).toBeNull();
});

it("stops a stale live card when status confirms idle and preserves it across remount", () => {
  let state = reduce(reduce(initialState, ev("turn_started")), ev("reasoning", { text: "分析" }));
  const view = render(cards(state.items));
  act(() => void vi.advanceTimersByTime(2300));
  state = reduce(state, ev("__running", { running: false }));
  view.rerender(cards(state.items));
  act(() => void vi.advanceTimersByTime(1_800_000));
  expect(labels()).toEqual(["思考 2.3 秒 · 2 字"]);
  view.unmount();
  const tick = vi.spyOn(window, "setInterval");
  render(cards(state.items));
  act(() => void vi.advanceTimersByTime(1000));
  expect(labels()).toEqual(["思考 2.3 秒 · 2 字"]);
  expect(tick).not.toHaveBeenCalled();
});
