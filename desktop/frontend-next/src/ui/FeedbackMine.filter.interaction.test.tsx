// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { listed, pick } from "./feedbacktestkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { FeedbackItem, FeedbackMine as Mine } from "../port/feedback";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const item = (over: Partial<FeedbackItem>): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "recorded", needsInput: false, underReview: false,
  replies: [], unreadReplies: 0, createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});
const reply = (id: number) => ({ id, author: "maintainer" as const, body: "hello", createdAt: "2026-09-22T00:00:00Z" });

const MINE: Mine = {
  offline: false, unread: 1, hasNew: true,
  items: [
    item({ receipt: "FB-NEW-0001", status: "in_progress", unreadReplies: 1, replies: [reply(1)] }),
    item({ receipt: "FB-ASK-0002", status: "needs_info", needsInput: true }),
    item({ receipt: "FB-OPEN-0003", status: "in_progress" }),
    item({ receipt: "FB-DONE-0004", status: "fixed" }),
    item({ receipt: "FB-DUP-0005", status: "duplicate" }),
  ],
};

function mount(mine: Mine = MINE) {
  const port = new MockPort() as unknown as AgentPort;
  port.myFeedback = vi.fn(async () => mine);
  render(<FeedbackMine port={port} onFile={() => {}} />);
  return port;
}
const shown = () => [...document.querySelectorAll(".fbk-item:not([hidden])")].map((li) => li.getAttribute("data-receipt"));
const chip = (name: RegExp) => screen.getByRole("button", { name });

describe("filtering my feedback", () => {
  it("counts each group on its chip and starts on all of them", async () => {
    mount();
    await listed("FB-NEW-0001");
    expect(chip(/^全部/).getAttribute("aria-pressed")).toBe("true");
    expect(chip(/^全部/).textContent).toContain("5");
    expect(chip(/^有新回复/).textContent).toContain("1");
    expect(chip(/^等你回复/).textContent).toContain("1");
    expect(chip(/^进行中/).textContent).toContain("3");
    expect(chip(/^已结束/).textContent).toContain("2");
    expect(shown()).toHaveLength(5);
  });

  it("narrows the list to one group and back", async () => {
    mount();
    await listed("FB-NEW-0001");
    await userEvent.click(chip(/^有新回复/));
    expect(shown()).toEqual(["FB-NEW-0001"]);
    expect(chip(/^有新回复/).getAttribute("aria-pressed")).toBe("true");
    await userEvent.click(chip(/^等你回复/));
    expect(shown()).toEqual(["FB-ASK-0002"]);
    await userEvent.click(chip(/^已结束/));
    expect(shown()).toEqual(["FB-DONE-0004", "FB-DUP-0005"]);
    await userEvent.click(chip(/^全部/));
    expect(shown()).toHaveLength(5);
  });

  it("keeps a reply visible under the new-replies filter after it has been read this visit", async () => {
    const port = mount();
    await listed("FB-NEW-0001");
    const seen = vi.spyOn(port, "feedbackSeen");
    await userEvent.click(chip(/^有新回复/));
    await waitFor(() => expect(seen).toHaveBeenCalledWith("FB-NEW-0001", 1));
    expect(shown()).toEqual(["FB-NEW-0001"]);
  });

  it("keeps a half-written reply when the filter moves away and back", async () => {
    mount();
    await listed("FB-OPEN-0003");
    const box = () => document.querySelector<HTMLTextAreaElement>('.fbk-detail[data-receipt="FB-OPEN-0003"] [data-action="feedback.reply.body"]')!;
    pick("FB-OPEN-0003");
    await userEvent.type(box(), "still typing");
    await userEvent.click(chip(/^已结束/));
    expect(shown()).not.toContain("FB-OPEN-0003");
    await userEvent.click(chip(/^全部/));
    expect(box().value).toBe("still typing");
  });

  it("says so when a group is empty instead of drawing a blank page", async () => {
    mount({ ...MINE, items: MINE.items.filter((i) => !i.unreadReplies) });
    await listed("FB-ASK-0002");
    await userEvent.click(chip(/^有新回复/));
    expect(shown()).toEqual([]);
    expect(screen.getByText("这一组里没有反馈。")).toBeTruthy();
  });

  it("draws no chips for a single report", async () => {
    mount({ ...MINE, items: [MINE.items[0]] });
    await listed("FB-NEW-0001");
    expect(screen.queryByRole("button", { name: /^全部/ })).toBeNull();
  });
});
