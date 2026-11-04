// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { listed, pick } from "./feedbacktestkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort } from "../port/port";
import { FEEDBACK_CODE, type FeedbackItem, type FeedbackMine as Mine } from "../port/feedback";

afterEach(cleanup);

const item = (over: Partial<FeedbackItem>): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "recorded", needsInput: false, underReview: false, replies: [], unreadReplies: 0,
  createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});
const mine = (items: FeedbackItem[]): Mine => ({ items, offline: false, unread: 0, hasNew: false });

function draw(items: FeedbackItem[], withdraw = vi.fn(async () => {})) {
  let current = items;
  const port = new MockPort() as unknown as AgentPort;
  port.myFeedback = vi.fn(async () => mine(current));
  port.feedbackSeen = vi.fn(async () => {});
  port.withdrawFeedback = vi.fn(async (receipt: string) => {
    await withdraw();
    current = current.map((i) => (i.receipt === receipt ? { ...i, status: "withdrawn" as const } : i));
  });
  render(<FeedbackMine port={port} onFile={() => {}} />);
  return port;
}

const more = () => screen.queryByRole("button", { name: "更多操作" });
const open = () => screen.queryByRole("button", { name: "撤回这条反馈…" });

describe("withdrawing a report", () => {
  it("offers it only where the service can still take a report back", async () => {
    draw([item({ receipt: "FB-AAAA-0001", status: "recorded" }), item({ receipt: "FB-AAAA-0002", status: "fixed" }), item({ receipt: "FB-AAAA-0003", status: "withdrawn" }), item({ receipt: "FB-AAAA-0004", status: "recorded", statusUnavailable: true })]);
    await listed("FB-AAAA-0004");
    const offered = ["FB-AAAA-0001", "FB-AAAA-0002", "FB-AAAA-0003", "FB-AAAA-0004"].filter((r) => { pick(r); return more() !== null; });
    expect(offered).toEqual(["FB-AAAA-0001"]);
    pick("FB-AAAA-0001");
    await userEvent.click(more()!);
    expect(open()).not.toBeNull();
  });

  it("says what it closes and sends nothing until the person agrees", async () => {
    const user = userEvent.setup();
    const port = draw([item({ issueNumber: 11350, issueUrl: "https://github.com/esengine/DeepSeek-Reasonix/issues/11350" })]);
    await waitFor(() => expect(more()).not.toBeNull());
    await user.click(more()!);
    await user.click(open()!);
    expect(screen.getByText(/对应的 #11350 议题会被关闭/)).toBeTruthy();
    expect(port.withdrawFeedback).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(port.withdrawFeedback).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "确认撤回" })).toBeNull();
  });

  it("withdraws on confirm and shows the report as withdrawn", async () => {
    const user = userEvent.setup();
    const port = draw([item({})]);
    await waitFor(() => expect(more()).not.toBeNull());
    await user.click(more()!);
    await user.click(open()!);
    await user.click(screen.getByRole("button", { name: "确认撤回" }));
    await waitFor(() => expect(port.withdrawFeedback).toHaveBeenCalledWith("FB-AAAA-0001"));
    await waitFor(() => expect(document.querySelector('.fbk-item[data-receipt="FB-AAAA-0001"]')?.getAttribute("data-status")).toBe("withdrawn"));
    expect(more()).toBeNull();
    expect(open()).toBeNull();
  });

  it("reads again when the service says the report already has an outcome", async () => {
    const user = userEvent.setup();
    const port = draw([item({})], vi.fn(async () => { throw new HttpError(409, "n", { code: FEEDBACK_CODE.notWithdrawable, error: "n" }); }));
    await waitFor(() => expect(more()).not.toBeNull());
    await user.click(more()!);
    await user.click(open()!);
    await user.click(screen.getByRole("button", { name: "确认撤回" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toMatch(/不能再撤回/));
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });
});
