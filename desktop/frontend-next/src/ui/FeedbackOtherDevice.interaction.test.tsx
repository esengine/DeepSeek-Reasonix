// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import "./testkit";
import { listed, pick } from "./feedbacktestkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { FeedbackItem, FeedbackMine as Mine } from "../port/feedback";

afterEach(cleanup);

const item = (over: Partial<FeedbackItem>): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "recorded", needsInput: false, underReview: false, replies: [], unreadReplies: 0,
  createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});

function draw(items: FeedbackItem[]) {
  const port = new MockPort() as unknown as AgentPort;
  const mine: Mine = { items, offline: false, unread: 0, hasNew: false };
  port.myFeedback = vi.fn(async () => mine);
  port.feedbackSeen = vi.fn(async () => {});
  render(<FeedbackMine port={port} onFile={() => {}} />);
}

describe("reports from another linked device", () => {
  it("marks them in words, and only them", async () => {
    draw([item({ receipt: "FB-AAAA-0001" }), item({ receipt: "FB-BBBB-0002", otherInstall: true, titleSnippet: "from the laptop" })]);
    await listed("FB-BBBB-0002");
    expect(within(pick("FB-BBBB-0002")).getByText("来自其他设备")).toBeTruthy();
    expect(within(pick("FB-AAAA-0001")).queryByText("来自其他设备")).toBeNull();
  });

  it("says read marks are kept on this computer only", async () => {
    draw([item({ otherInstall: true })]);
    await waitFor(() => expect(screen.getAllByText("FB-AAAA-0001").length).toBeGreaterThan(0));
    expect(screen.getByText(/已读标记只记在这台电脑上/)).toBeTruthy();
  });
});
