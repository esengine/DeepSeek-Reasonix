// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "../testkit";
import { SayCard } from "./SayCard";

afterEach(() => {
  window.getSelection()?.removeAllRanges();
  cleanup();
  vi.useRealTimers();
});

const item = { t: "say" as const, id: "answer-1", text: "First chosen sentence last.", done: true };

async function card(over = {}) {
  const onQuote = vi.fn();
  const view = render(<SayCard item={{ ...item, ...over }} reply={{ onQuote, canRegenerate: () => false, hasLaterTurns: () => false, onRegenerate: vi.fn(), canBranch: () => false, onBranch: vi.fn() }} />);
  await waitFor(() => expect(view.container.querySelector(".txt p")).toBeTruthy());
  vi.useFakeTimers();
  const answer = view.container.querySelector<HTMLElement>(".txt")!;
  return { answer, text: answer.querySelector("p")!.firstChild!, onQuote };
}

function select(node: Node, from: number, to: number) {
  const range = document.createRange();
  range.setStart(node, from);
  range.setEnd(node, to);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
}

const settle = () => act(() => { vi.advanceTimersByTime(130); });

it("raises quote and copy under a selection once the pointer lets go", async () => {
  const { answer, text, onQuote } = await card();
  select(text, 6, 21);
  fireEvent.mouseUp(answer);
  expect(screen.queryByRole("toolbar")).toBeNull();
  settle();
  const bar = screen.getByRole("toolbar");
  expect([...bar.querySelectorAll("button")].map((b) => b.textContent)).toEqual(["引用选中内容", "复制选中内容"]);
  fireEvent.click(screen.getByRole("button", { name: "引用选中内容" }));
  expect(onQuote).toHaveBeenCalledWith("chosen sentence", item.id);
  expect(screen.queryByRole("toolbar")).toBeNull();
});

it("raises nothing for a click that selects nothing", async () => {
  const { answer } = await card();
  fireEvent.mouseUp(answer);
  settle();
  expect(screen.queryByRole("toolbar")).toBeNull();
});

it("gives way to the context menu", async () => {
  const { answer, text } = await card();
  select(text, 6, 21);
  fireEvent.mouseUp(answer);
  settle();
  fireEvent.contextMenu(answer, { clientX: 20, clientY: 20 });
  expect(screen.queryByRole("toolbar")).toBeNull();
  expect(screen.getByRole("menu")).toBeTruthy();
});

it("offers nothing on an answer still streaming", async () => {
  const { answer, text } = await card({ done: false });
  select(text, 0, 5);
  fireEvent.mouseUp(answer);
  settle();
  expect(screen.queryByRole("toolbar")).toBeNull();
});
