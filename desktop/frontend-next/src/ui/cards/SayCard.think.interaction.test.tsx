// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "../testkit";
import { SayCard } from "./SayCard";
import { appendText } from "../../state/say";
import { setFoldModes } from "../../state/prefs";
import type { Item } from "../../state/session";

afterEach(() => { cleanup(); vi.useRealTimers(); });

type Say = Extract<Item, { t: "say" }>;
const think = () => document.querySelector<HTMLDetailsElement>("details.think")!;
const label = () => think().querySelector(".think-label:not(.think-out)")!;

describe("the thinking block, as the Think-Exec board draws it", () => {
  it("sweeps its title only while the model is still thinking", () => {
    const view = render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    expect(think().hasAttribute("data-live")).toBe(true);
    expect(label().classList.contains("think-sweep")).toBe(true);
    view.rerender(<SayCard item={{ t: "say", id: "a", text: "答", reasoning: "想", done: false, thoughtMs: 1200 }} />);
    expect(think().hasAttribute("data-live")).toBe(false);
    expect(label().classList.contains("think-sweep")).toBe(false);
    expect(label().textContent).toBe("思考 1.2 秒 · 1 字");
  });

  it("tells the stylesheet when the reader asked for every thought open, so the live window does not cap it", () => {
    act(() => setFoldModes({ thinking: "open" }));
    const view = render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    expect(think().hasAttribute("data-live")).toBe(true);
    expect(think().hasAttribute("data-full")).toBe(true);
    view.unmount();
    act(() => setFoldModes({ thinking: "live" }));
    render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    expect(think().hasAttribute("data-full")).toBe(false);
    act(() => setFoldModes({ thinking: "folded" }));
  });

  it("keeps a caret only on reasoning still arriving", () => {
    const view = render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    think().open = true;
    view.rerender(<SayCard item={{ t: "say", id: "a", text: "", reasoning: "想", done: false, thoughtMs: 900 }} />);
    expect(think().querySelector(".caret")).toBeNull();
  });

  it("says why a thought stopped halfway, and that what was thought is still inside", () => {
    render(<SayCard item={{ t: "say", id: "a", text: "", reasoning: "想到一半", done: true, thoughtMs: 3100, cut: "stopped" }} />);
    expect(think().getAttribute("data-cut")).toBe("stopped");
    expect(label().textContent).toBe("思考到一半停下了 · 3.1 秒");
    expect(think().querySelector(".think-note")!.textContent).toBe("你点了「停下」；已经想到的内容展开可以看");
  });

  it("names a turn that failed mid-thought apart from one that was stopped", () => {
    render(<SayCard item={{ t: "say", id: "a", text: "", reasoning: "想到一半", done: true, thoughtMs: 2000, cut: "failed" }} />);
    expect(label().textContent).toBe("思考到一半出错了 · 2.0 秒");
  });

  it("does not sweep in from nothing when a finished thought is drawn again", () => {
    render(<SayCard item={{ t: "say", id: "a", text: "答", reasoning: "想", done: true, thoughtMs: 1000 }} />);
    expect(think().hasAttribute("data-settled")).toBe(false);
  });

  it("marks the title change once when a thought it watched finishes", () => {
    const view = render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    view.rerender(<SayCard item={{ t: "say", id: "a", text: "答", reasoning: "想", done: true, thoughtMs: 1000 }} />);
    expect(think().hasAttribute("data-settled")).toBe(true);
  });

  it("crossfades the running title out and the settled one in, then keeps only the settled one", () => {
    vi.useFakeTimers();
    const view = render(<SayCard item={appendText([], "想", "reasoning")[0] as Say} />);
    view.rerender(<SayCard item={{ t: "say", id: "a", text: "答", reasoning: "想", done: true, thoughtMs: 6200 }} />);
    const labels = () => [...think().querySelectorAll(".think-label")].map((l) => [l.classList.contains("think-out"), l.textContent]);
    expect(labels()).toEqual([[true, "思考中 · 6.2 秒"], [false, "思考 6.2 秒 · 1 字"]]);
    act(() => void vi.advanceTimersByTime(300));
    expect(labels()).toEqual([[false, "思考 6.2 秒 · 1 字"]]);
  });
});
