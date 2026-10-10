// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import type { FeedbackItem, FeedbackMine as Mine } from "../port/feedback";

afterEach(cleanup);

const item = (receipt = "FB-AAAA-0001", unread = 1, count = 2): FeedbackItem => ({
  receipt, category: "bug", titleSnippet: "A report", status: "answered", needsInput: false, underReview: false,
  unreadReplies: unread, createdAt: "2026-10-01T08:00:00Z", updatedAt: "2026-10-02T08:00:00Z",
  replies: Array.from({ length: count }, (_, i) => ({ id: i + 1, author: "maintainer", body: `Reply ${i + 1}`, createdAt: "2026-10-02T08:00:00Z" })),
});

const mine = (items: FeedbackItem[]): Mine => ({ items, offline: false, unread: items.filter((i) => i.unreadReplies > 0 || i.needsInput).length, hasNew: false });
const row = (receipt = "FB-AAAA-0001") => screen.getByText(receipt).closest("li")!;
const toggle = (receipt = "FB-AAAA-0001") => within(row(receipt)).getByRole("button", { name: /展开对话|收起对话/ });

function setup(read: () => Mine) {
  const port = new MockPort();
  vi.spyOn(port, "myFeedback").mockImplementation(async () => read());
  const seen = vi.spyOn(port, "feedbackSeen").mockResolvedValue(undefined);
  vi.spyOn(port, "replyFeedback");
  const onUnread = vi.fn();
  render(<FeedbackMine port={port} onFile={vi.fn()} onUnread={onUnread} />);
  return { port, seen, onUnread };
}

describe("folded feedback conversations", () => {
  it("starts folded, exposes reply counts and leaves hidden replies unread until opened", async () => {
    const { seen, onUnread } = setup(() => mine([item()]));
    await screen.findByText("FB-AAAA-0001");
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(toggle().textContent).toContain("2 条回复");
    expect(toggle().textContent).toContain("1 条新回复");
    expect(within(row().querySelector<HTMLElement>(".fbk-thread")!).queryByRole("list")).toBeNull();
    expect(seen).not.toHaveBeenCalled();
    expect(onUnread).toHaveBeenLastCalledWith(1);

    await userEvent.click(toggle());
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(within(row().querySelector<HTMLElement>(".fbk-thread")!).getByRole("list")).toBeTruthy();
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 2));
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(0));
    await userEvent.click(toggle());
    expect(toggle().textContent).toContain("1 条新回复");
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it("opens and closes one conversation from the keyboard without opening its neighbour", async () => {
    setup(() => mine([item(), item("FB-AAAA-0002", 0)]));
    await screen.findByText("FB-AAAA-0002");
    toggle().focus();
    await userEvent.keyboard("{Enter}");
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(toggle("FB-AAAA-0002").getAttribute("aria-expanded")).toBe("false");
    await userEvent.keyboard(" ");
    expect(toggle().getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(toggle());
  });

  it("keeps newly refreshed replies unread while folded, then reads them on opening", async () => {
    let count = 2;
    const { seen, onUnread } = setup(() => mine([item("FB-AAAA-0001", count - 1, count)]));
    await screen.findByText("FB-AAAA-0001");
    count = 3;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(toggle().textContent).toContain("2 条新回复"));
    expect(seen).not.toHaveBeenCalled();
    expect(onUnread).toHaveBeenLastCalledWith(1);
    await userEvent.click(toggle());
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 3));
  });

  it("waits for earlier unread replies too, and preserves the full view through a refresh", async () => {
    let count = 6;
    const { seen } = setup(() => mine([item("FB-AAAA-0001", count, count)]));
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(toggle());
    expect(seen).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "显示更早的 2 条" }));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 6));
    count = 7;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 7));
    expect(toggle().getAttribute("aria-expanded")).toBe("true");
    expect(within(row()).getAllByText(/Reply \d/)).toHaveLength(7);
  });

  it("retains a reply draft when its conversation is folded and expanded again", async () => {
    const { port } = setup(() => mine([item()]));
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(screen.getByRole("button", { name: "回复" }));
    const box = screen.getByRole<HTMLTextAreaElement>("textbox", { name: /回复/ });
    await userEvent.type(box, "My draft");
    await userEvent.click(toggle());
    await userEvent.click(toggle());
    expect(box.value).toBe("My draft");
    expect(port.replyFeedback).not.toHaveBeenCalled();
  });

  it("does not let an earlier read clear replies that arrived after the thread was folded", async () => {
    let count = 2;
    const { seen, onUnread } = setup(() => mine([item("FB-AAAA-0001", 1, count)]));
    let finish!: () => void;
    seen.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(toggle());
    await userEvent.click(toggle());
    count = 3;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(toggle().textContent).toContain("3 条回复"));
    await act(async () => finish());
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(1));
    await userEvent.click(toggle());
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 3));
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(0));
  });
});
