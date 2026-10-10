// @vitest-environment jsdom
import { act, cleanup, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "../testkit";
import { FOLD_DEFAULTS, setFoldModes } from "../../state/prefs";
import { appendText, foldMessage } from "../../state/say";
import type { Item } from "../../state/session";
import { PaneShown } from "../shown";
import { SayCard } from "./SayCard";

const frames = new Map<number, FrameRequestCallback>();
let requested = 0;
let canceled = 0;

beforeEach(() => {
  setFoldModes(FOLD_DEFAULTS);
  frames.clear();
  requested = 0;
  canceled = 0;
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    frames.set(++requested, cb);
    return requested;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    if (frames.delete(id)) canceled++;
  });
});

afterEach(() => {
  cleanup();
  setFoldModes(FOLD_DEFAULTS);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function drain() {
  act(() => {
    let delivered = 0;
    while (frames.size) {
      if (++delivered > 1000) throw new Error("reveal did not settle");
      const [id, cb] = frames.entries().next().value as [number, FrameRequestCallback];
      frames.delete(id);
      cb(performance.now());
    }
  });
}

function draw(shown = true) {
  let items = appendText([], "考虑中", "reasoning");
  const card = () => <PaneShown.Provider value={shown}><SayCard item={items[0] as Extract<Item, { t: "say" }>} /></PaneShown.Provider>;
  const view = render(card());
  return {
    details: view.container.querySelector("details") as HTMLDetailsElement,
    text: () => view.container.querySelector(".tk")?.textContent,
    reasoning: () => (items[0] as Extract<Item, { t: "say" }>).reasoning,
    append: (text: string, field: "text" | "reasoning" = "reasoning") => {
      items = appendText(items, text, field);
      view.rerender(card());
    },
    show: (next: boolean) => { shown = next; view.rerender(card()); },
    finish: () => { items = foldMessage(items, { kind: "message" }); view.rerender(card()); },
  };
}

describe("thinking text reveal", () => {
  it("requests no frames for bursts arriving in a folded thought", () => {
    const tick = vi.spyOn(window, "setInterval");
    const card = draw();
    expect(card.details.open).toBe(false);
    for (const chunk of ["分析".repeat(60), "继续".repeat(60), "结论".repeat(60)]) {
      card.append(chunk);
      drain();
    }
    expect(card.text()).toBe(card.reasoning());
    expect(requested).toBe(0);
    expect(tick).toHaveBeenCalledOnce();
  });

  it.each(["live", "open"] as const)("stops on a user fold under %s and resumes from the latest text", async (mode) => {
    setFoldModes({ thinking: mode });
    const card = draw();
    const summary = card.details.querySelector("summary") as HTMLElement;
    expect(card.details.open).toBe(true);
    card.append("展开时到达的内容".repeat(20));
    expect(frames.size).toBe(1);
    expect(card.text()).not.toBe(card.reasoning());

    await userEvent.click(summary);
    await waitFor(() => expect(frames.size).toBe(0));
    expect(card.details.open).toBe(false);
    expect(canceled).toBe(1);
    const before = requested;
    card.append("折叠时到达的内容".repeat(20));
    expect(requested).toBe(before);
    expect(card.text()).toBe(card.reasoning());

    await userEvent.click(summary);
    expect(card.details.open).toBe(true);
    expect(card.text()).toBe(card.reasoning());
    expect(frames.size).toBe(0);
    card.append("重新展开后的新内容".repeat(20));
    expect(frames.size).toBe(1);
    expect(card.text()).not.toBe(card.reasoning());
    drain();
    expect(card.text()).toBe(card.reasoning());
    expect(frames.size).toBe(0);
  });

  it("cancels a live thought's backlog when the first answer folds it", () => {
    setFoldModes({ thinking: "live" });
    const card = draw();
    card.append("分析".repeat(60));
    expect(frames.size).toBe(1);
    card.append("答", "text");
    expect(card.details.open).toBe(false);
    expect(frames.size).toBe(0);
    expect(canceled).toBe(1);
    expect(card.text()).toBe(card.reasoning());
  });

  it("keeps expanded thoughts current while their pane is hidden", () => {
    setFoldModes({ thinking: "open" });
    const card = draw();
    card.append("分析".repeat(60));
    expect(frames.size).toBe(1);
    card.show(false);
    expect(frames.size).toBe(0);
    const before = requested;
    card.append("后台".repeat(60));
    expect(requested).toBe(before);
    expect(card.text()).toBe(card.reasoning());
    card.show(true);
    expect(frames.size).toBe(0);
    card.append("前台".repeat(60));
    expect(frames.size).toBe(1);
    drain();
    expect(card.text()).toBe(card.reasoning());
  });

  it("settles an expanded thought immediately on completion", () => {
    setFoldModes({ thinking: "open" });
    const card = draw();
    card.append("分析".repeat(60));
    expect(frames.size).toBe(1);
    card.finish();
    expect(card.details.open).toBe(true);
    expect(frames.size).toBe(0);
    expect(card.text()).toBe(card.reasoning());
    expect(card.details.querySelector(".caret")).toBeNull();
  });

  it("shows expanded bursts immediately with reduced motion", () => {
    setFoldModes({ thinking: "open" });
    const media = matchMedia;
    vi.stubGlobal("matchMedia", (query: string) => ({ ...media(query), matches: true }));
    const card = draw();
    card.append("分析".repeat(60));
    expect(card.details.open).toBe(true);
    expect(card.text()).toBe(card.reasoning());
    expect(requested).toBe(0);
  });
});
