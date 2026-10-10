// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { SayCard } from "./SayCard";
import { useReplyActions } from "../reply";
import type { AgentPort } from "../../port/port";
import type { Item } from "../../state/session";

afterEach(() => {
  window.getSelection()?.removeAllRanges();
  cleanup();
  vi.restoreAllMocks();
});

const item = { t: "say" as const, id: "answer-1", text: "First chosen sentence last.", done: true };
const QUOTE = "引用选中内容";

function select(start: Node, from: number, end = start, to = start.textContent!.length) {
  const range = document.createRange();
  range.setStart(start, from);
  range.setEnd(end, to);
  const selection = window.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
}

async function card(over = {}) {
  const onQuote = vi.fn();
  const view = render(<SayCard item={{ ...item, ...over }} reply={{ onQuote, canRegenerate: () => false, hasLaterTurns: () => false, onRegenerate: vi.fn() }} />);
  await waitFor(() => expect(view.container.querySelector(".txt p")).toBeTruthy());
  const answer = view.container.querySelector<HTMLElement>(".txt")!;
  const text = answer.querySelector("p")!.firstChild!;
  return { ...view, answer, text, onQuote };
}

describe("quoting a selection from an assistant reply", () => {
  it("captures the right-clicked selection before menu focus clears it", async () => {
    const { answer, text, onQuote } = await card();
    select(text, 6, text, 21);
    expect(fireEvent.contextMenu(answer, { clientX: 240, clientY: 180 })).toBe(false);
    const quote = screen.getByRole("menuitem", { name: QUOTE });
    expect(quote.closest('[role="menu"]')!.parentElement).toBe(document.body);
    window.getSelection()!.removeAllRanges();
    await userEvent.click(quote);
    expect(onQuote).toHaveBeenCalledWith("chosen sentence", item.id);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("quotes a multiline code selection without the surrounding answer", async () => {
    const { answer, onQuote } = await card({ text: "First paragraph.\n\n```text\nalpha\n  beta\ngamma\n```" });
    const code = await screen.findByText("alpha beta gamma", { selector: "code.hljs" });
    select(code.firstChild!, 0, code.firstChild!, 12);
    fireEvent.contextMenu(code);
    await userEvent.click(screen.getByRole("menuitem", { name: QUOTE }));
    expect(onQuote).toHaveBeenCalledWith("alpha\n  beta", item.id);
    expect(answer.textContent).toContain("First paragraph.");
  });

  it("leaves the native menu for an empty selection, reasoning, streaming text and a cross-card selection", async () => {
    const first = await card({ reasoning: "Private reasoning" });
    expect(fireEvent.contextMenu(first.answer)).toBe(true);
    select(first.text, 0, first.text, 5);
    expect(fireEvent.contextMenu(first.container.querySelector(".tk")!)).toBe(true);
    const second = await card({ id: "answer-2", text: "Other reply.", done: false });
    select(second.text, 0, second.text, 5);
    expect(fireEvent.contextMenu(second.answer)).toBe(true);
    select(first.text, 6, second.text, 5);
    expect(fireEvent.contextMenu(first.answer)).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(first.onQuote).not.toHaveBeenCalled();
  });

  it("opens from Shift+F10, quotes with Enter, and restores focus on Escape", async () => {
    const { answer, text, onQuote } = await card();
    answer.focus();
    select(text, 6, text, 21);
    fireEvent.keyDown(answer, { key: "F10", shiftKey: true });
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: QUOTE }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(answer);
    select(text, 6, text, 21);
    fireEvent.keyDown(answer, { key: "F10", shiftKey: true });
    await userEvent.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "复制选中内容" }));
    await userEvent.keyboard("{Home}");
    await userEvent.keyboard("{Enter}");
    expect(onQuote).toHaveBeenCalledWith("chosen sentence", item.id);
  });

  it("shows a clipboard refusal without losing the selection's quote action", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error("denied"));
    const { answer, text, onQuote } = await card();
    select(text, 6, text, 21);
    fireEvent.contextMenu(answer);
    await user.click(screen.getByRole("menuitem", { name: "复制选中内容" }));
    await screen.findByText("复制失败，请重试");
    await user.click(screen.getByRole("menuitem", { name: QUOTE }));
    expect(onQuote).toHaveBeenCalledWith("chosen sentence", item.id);
  });

  it("keeps the original quote button for both selected text and a whole answer", async () => {
    const { text, onQuote } = await card();
    select(text, 6, text, 21);
    fireEvent.click(screen.getByRole("button", { name: "引用到输入框" }));
    expect(onQuote).toHaveBeenLastCalledWith("chosen sentence", item.id);
    window.getSelection()!.removeAllRanges();
    fireEvent.click(screen.getByRole("button", { name: "引用到输入框" }));
    expect(onQuote).toHaveBeenLastCalledWith(item.text, item.id);
  });

  it("keeps copying available and dismisses the menu on an outside press", async () => {
    const user = userEvent.setup();
    const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
    const { answer, text, onQuote } = await card();
    select(text, 6, text, 21);
    fireEvent.contextMenu(answer);
    await user.click(screen.getByRole("menuitem", { name: "复制选中内容" }));
    await waitFor(() => expect(write).toHaveBeenCalledWith("chosen sentence"));
    expect(onQuote).not.toHaveBeenCalled();
    await user.click(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("keeps the selection attached to its checkpoint through the existing quote path", async () => {
    const items: Item[] = [{ t: "user", id: "u", text: "Question", msgIndex: 3 }, item];
    function Replies() {
      const { reply, quote } = useReplyActions({
        port: {} as AgentPort, items, checkpoints: [{ turn: 7, msgIndex: 3, prompt: "Question", files: 0 }], running: false,
        submit: async () => true, reloadSession: async () => {}, onSettings: vi.fn(), onRunDetail: vi.fn(), onError: vi.fn(),
      });
      return <><SayCard item={item} reply={reply} /><output data-testid="quote">{JSON.stringify(quote)}</output></>;
    }
    const { container } = render(<Replies />);
    await waitFor(() => expect(container.querySelector(".txt p")).toBeTruthy());
    const answer = container.querySelector<HTMLElement>(".txt")!;
    const text = answer.querySelector("p")!.firstChild!;
    select(text, 6, text, 21);
    fireEvent.contextMenu(answer);
    await userEvent.click(screen.getByRole("menuitem", { name: QUOTE }));
    expect(JSON.parse(screen.getByTestId("quote").textContent!)).toEqual({ text: "chosen sentence", turn: 7, n: 1 });
  });
});
