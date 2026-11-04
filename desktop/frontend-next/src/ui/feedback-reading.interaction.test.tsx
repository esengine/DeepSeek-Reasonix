// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { FeedbackMine } from "./FeedbackMine";
import { listed, rowOf } from "./feedbacktestkit";
import { MockPort } from "../port/mock";
import type { FeedbackItem, FeedbackMine as Mine } from "../port/feedback";

afterEach(cleanup);

const item = (receipt = "FB-AAAA-0001", unread = 1, count = 2): FeedbackItem => ({
  receipt, category: "bug", titleSnippet: `Report ${receipt}`, status: "answered", needsInput: false, underReview: false,
  unreadReplies: unread, createdAt: "2026-10-01T08:00:00Z", updatedAt: "2026-10-02T08:00:00Z",
  replies: Array.from({ length: count }, (_, i) => ({ id: i + 1, author: "maintainer", body: `Reply ${i + 1}`, createdAt: "2026-10-02T08:00:00Z" })),
});

const mine = (items: FeedbackItem[]): Mine => ({ items, offline: false, unread: items.filter((i) => i.unreadReplies > 0 || i.needsInput).length, hasNew: false });

function setup(read: () => Mine) {
  const port = new MockPort();
  vi.spyOn(port, "myFeedback").mockImplementation(async () => read());
  const seen = vi.spyOn(port, "feedbackSeen").mockResolvedValue(undefined);
  const onUnread = vi.fn();
  render(<FeedbackMine port={port} onFile={vi.fn()} onUnread={onUnread} />);
  return { port, seen, onUnread };
}

describe("reading a report's replies", () => {
  it("reads the report on the right and leaves the others unread until they are opened", async () => {
    const { seen, onUnread } = setup(() => mine([item(), item("FB-AAAA-0002", 1, 3)]));
    await listed("FB-AAAA-0002");
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 2));
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(1));
    expect(seen).not.toHaveBeenCalledWith("FB-AAAA-0002", expect.anything());
    expect(document.querySelector('.fbk-item[data-receipt="FB-AAAA-0002"]')!.hasAttribute("data-fresh")).toBe(true);
    await userEvent.click(rowOf("FB-AAAA-0002"));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0002", 3));
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(0));
  });

  it("opens a report from the keyboard", async () => {
    setup(() => mine([item(), item("FB-AAAA-0002", 0)]));
    await listed("FB-AAAA-0002");
    rowOf("FB-AAAA-0002").focus();
    await userEvent.keyboard("{Enter}");
    expect(document.querySelector(".fbk-detail")!.getAttribute("data-receipt")).toBe("FB-AAAA-0002");
    expect(rowOf("FB-AAAA-0002").getAttribute("aria-current")).toBe("true");
    expect(rowOf("FB-AAAA-0001").hasAttribute("aria-current")).toBe(false);
  });

  it("reads a reply a refresh brings to the report already open", async () => {
    let count = 2;
    const { seen } = setup(() => mine([item("FB-AAAA-0001", count - 1, count)]));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 2));
    count = 3;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 3));
    expect(screen.getAllByText(/Reply \d/)).toHaveLength(3);
  });

  it("does not let an earlier read clear a reply that arrived while it was in flight", async () => {
    let count = 2;
    const { seen, onUnread } = setup(() => mine([item("FB-AAAA-0001", 1, count)]));
    let finish!: () => void;
    seen.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 2));
    seen.mockImplementationOnce(() => new Promise<void>(() => {}));
    count = 3;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-AAAA-0001", 3));
    await act(async () => finish());
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(1));
  });
});
