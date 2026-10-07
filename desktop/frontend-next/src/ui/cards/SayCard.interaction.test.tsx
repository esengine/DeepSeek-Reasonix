// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SayCard } from "./SayCard";
import { FOLD_DEFAULTS, setFoldModes } from "../../state/prefs";

afterEach(() => {
  cleanup();
  setFoldModes(FOLD_DEFAULTS);
  vi.restoreAllMocks();
});

it("forks only an eligible final reply and blocks repeated clicks while opening", async () => {
  let finish = () => {};
  const onFork = vi.fn(() => new Promise<void>((resolve) => { finish = resolve; }));
  const reply = { onQuote: vi.fn(), canRegenerate: () => false, hasLaterTurns: () => false, onRegenerate: vi.fn(), onFork, forkable: new Set(["final"]) };
  const { rerender } = render(<SayCard item={{ t: "say", id: "middle", text: "checking", done: true }} reply={reply} />);
  expect(screen.queryByRole("button", { name: "从此回复创建对话分支" })).toBeNull();
  rerender(<SayCard item={{ t: "say", id: "final", text: "answer", done: true }} reply={reply} />);
  const button = screen.getByRole("button", { name: "从此回复创建对话分支" }) as HTMLButtonElement;
  await userEvent.click(button);
  await userEvent.click(button);
  expect(onFork).toHaveBeenCalledExactlyOnceWith("final");
  expect(button.disabled).toBe(true);
  await act(async () => finish());
});

describe("completed reasoning", () => {
  it("starts folded and opens on click", async () => {
    const { container } = render(<SayCard item={{ t: "say", id: "s", text: "answer", reasoning: "reason", done: true }} />);
    const details = container.querySelector("details") as HTMLDetailsElement;
    expect(details.open).toBe(false);
    await userEvent.click(screen.getByText(/思考/));
    expect(details.open).toBe(true);
  });
});

describe("the folding preference", () => {
  const item = { t: "say" as const, id: "s", text: "answer", reasoning: "reason", done: true };
  const details = (el: HTMLElement) => el.querySelector("details") as HTMLDetailsElement;

  it("opens thinking by default once it is set to open", () => {
    setFoldModes({ thinking: "open" });
    expect(details(render(<SayCard item={item} />).container).open).toBe(true);
  });

  it("moves an untouched block and leaves a clicked one alone", async () => {
    const a = details(render(<SayCard item={item} />).container);
    const b = details(render(<SayCard item={{ ...item, id: "t" }} />).container);
    await userEvent.click(b.querySelector("summary") as HTMLElement);
    await userEvent.click(b.querySelector("summary") as HTMLElement);
    expect(b.open).toBe(false);
    act(() => setFoldModes({ thinking: "open" }));
    expect(a.open).toBe(true);
    expect(b.open).toBe(false);
  });

  it("under live, opens while thinking streams and folds once the answer starts", () => {
    window.matchMedia ??= (() => ({ matches: true, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
    setFoldModes({ thinking: "live" });
    const streaming = { t: "say" as const, id: "l", text: "", reasoning: "reason", done: false };
    const { container, rerender } = render(<SayCard item={streaming} />);
    expect(details(container).open).toBe(true);
    rerender(<SayCard item={{ ...streaming, text: "answer" }} />);
    expect(details(container).open).toBe(false);
  });
});

// jsdom lays nothing out, so the trigger's place on screen and the list's
// height are stated here; what is under test is the side the list opens on.
describe("the reply menus", () => {
  const item = { t: "say" as const, id: "s", text: "answer", done: true };
  const HEIGHT = 120;
  const TRIGGER = 28;
  let top = 0;
  const rect = (y: number, width: number, height: number) =>
    ({ x: 200, y, left: 200, top: y, width, height, right: 200 + width, bottom: y + height, toJSON() {} }) as DOMRect;

  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.matches('[data-action="reply.retry"], [data-action="reply.more"]')) return rect(top, TRIGGER, TRIGGER);
      if (this.classList.contains("acts-pop")) return rect(0, 200, HEIGHT);
      return rect(0, 0, 0);
    });
    vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(function (this: HTMLElement) {
      return this.classList.contains("acts-pop") ? HEIGHT : 0;
    });
  });

  const card = (onRegenerate = () => {}) =>
    render(<SayCard item={item} reply={{ onQuote: () => {}, canRegenerate: () => true, hasLaterTurns: () => false, onRegenerate, onConfigureModel: () => {}, onRunDetail: () => {} }} />);
  const list = () => document.querySelector<HTMLElement>(".acts-pop");

  for (const action of ["reply.retry", "reply.more"]) {
    describe(action, () => {
      const open = async () => userEvent.click(document.querySelector(`[data-action="${action}"]`) as HTMLElement);

      it("opens above the row when the list fits there", async () => {
        top = 500;
        card();
        await open();
        expect(list()?.style.top).toBe(`${500 - 5 - HEIGHT}px`);
      });

      it("opens below the row when the top edge would cut it off", async () => {
        top = 40;
        card();
        await open();
        expect(list()?.style.top).toBe(`${40 + TRIGGER + 5}px`);
      });

      it("is drawn outside the card, so neither the card nor the scroller clips it", async () => {
        top = 500;
        const { container } = card();
        await open();
        expect(list()?.parentElement).toBe(document.body);
        expect(container.contains(list())).toBe(false);
      });

      it("turns over when the conversation scrolls the row to the top", async () => {
        top = 500;
        card();
        await open();
        top = 40;
        fireEvent.scroll(window);
        expect(list()?.style.top).toBe(`${40 + TRIGGER + 5}px`);
      });
    });
  }

  it("keeps the list between the trigger and the next control in the tab order", async () => {
    top = 500;
    card();
    const retry = document.querySelector('[data-action="reply.retry"]') as HTMLElement;
    const more = document.querySelector('[data-action="reply.more"]') as HTMLElement;
    await userEvent.click(retry);
    expect(document.activeElement).toBe(retry);
    await userEvent.tab();
    expect(document.activeElement).toBe(document.querySelector('[data-action="reply.retry-now"]'));
    await userEvent.tab();
    expect(document.activeElement).toBe(document.querySelector('[data-action="reply.configure"]'));
    await userEvent.tab();
    expect(document.activeElement).toBe(more);
    await userEvent.tab({ shift: true });
    expect(document.activeElement).toBe(document.querySelector('[data-action="reply.configure"]'));
    await userEvent.tab({ shift: true });
    await userEvent.tab({ shift: true });
    expect(document.activeElement).toBe(retry);
  });

  it("closes on Escape and hands the focus back to its trigger", async () => {
    top = 500;
    card();
    const retry = document.querySelector('[data-action="reply.retry"]') as HTMLElement;
    await userEvent.click(retry);
    await userEvent.tab();
    await userEvent.keyboard("{Escape}");
    expect(list()).toBeNull();
    expect(document.activeElement).toBe(retry);
  });

  it("closes on a press outside and runs a chosen item", async () => {
    top = 500;
    const regenerate = vi.fn();
    card(regenerate);
    await userEvent.click(document.querySelector('[data-action="reply.retry"]') as HTMLElement);
    await userEvent.click(document.body);
    expect(list()).toBeNull();
    await userEvent.click(document.querySelector('[data-action="reply.retry"]') as HTMLElement);
    await userEvent.click(document.querySelector('[data-action="reply.retry-now"]') as HTMLElement);
    expect(regenerate).toHaveBeenCalledOnce();
    expect(list()).toBeNull();
  });
});
