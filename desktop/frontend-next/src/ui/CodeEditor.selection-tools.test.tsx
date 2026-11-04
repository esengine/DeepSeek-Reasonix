// @vitest-environment jsdom
import { EditorView } from "@codemirror/view";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import CodeEditor from "./CodeEditor";

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(EditorView.prototype, "coordsAtPos").mockReturnValue({ left: 40, right: 41, top: 10, bottom: 24 });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const view = () => EditorView.findFromDOM(document.querySelector(".cm-editor") as HTMLElement)!;
const select = (anchor: number, head: number) => act(() => { view().dispatch({ selection: { anchor, head } }); });
const settle = () => act(() => { vi.advanceTimersByTime(130); });
const doc = "one\ntwo\nthree\nfour";

function mount(onQuote = vi.fn()) {
  render(<CodeEditor path="a.ts" value={doc} dark={false} onChange={() => {}} onSave={() => {}} onQuote={onQuote} />);
  return onQuote;
}

it("offers the selected lines to the conversation once the selection settles", () => {
  const onQuote = mount();
  select(4, 13);
  expect(screen.queryByRole("toolbar")).toBeNull();
  settle();
  const add = screen.getByRole("button", { name: /加入对话/ });
  expect(screen.getByRole("toolbar").textContent).toContain("第 2–3 行");
  fireEvent.click(add);
  expect(onQuote).toHaveBeenCalledWith("two\nthree");
  expect(screen.queryByRole("toolbar")).toBeNull();
});

it("names a single line when the selection stays on one", () => {
  mount();
  select(0, 3);
  settle();
  expect(screen.getByRole("toolbar").textContent).toContain("第 1 行");
});

it("leaves out a line the selection only reaches the start of", () => {
  mount();
  select(0, 4);
  settle();
  expect(screen.getByRole("toolbar").textContent).toContain("第 1 行");
});

it("goes away when the selection collapses", () => {
  mount();
  select(0, 3);
  settle();
  select(2, 2);
  expect(screen.queryByRole("toolbar")).toBeNull();
});

it("shows nothing where no conversation takes a quote", () => {
  render(<CodeEditor path="a.ts" value={doc} dark={false} onChange={() => {}} onSave={() => {}} />);
  select(0, 3);
  settle();
  expect(screen.queryByRole("toolbar")).toBeNull();
});

it("sits above the selection, and drops below only where the top has no room", () => {
  mount();
  select(0, 3);
  settle();
  expect(screen.getByRole("toolbar").hasAttribute("data-below")).toBe(true);
  vi.spyOn(EditorView.prototype, "coordsAtPos").mockReturnValue({ left: 40, right: 41, top: 200, bottom: 214 });
  select(4, 7);
  settle();
  const bar = screen.getByRole("toolbar");
  expect(bar.hasAttribute("data-below")).toBe(false);
  expect(bar.style.top).toBe("194px");
});
