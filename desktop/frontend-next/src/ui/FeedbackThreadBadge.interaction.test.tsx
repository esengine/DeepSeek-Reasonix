// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { FeedbackItem, FeedbackMine as Mine, FeedbackProfile } from "../port/feedback";

afterEach(cleanup);

const reply = (id: number, author: "user" | "maintainer") => ({ id, author, body: `m${id}`, createdAt: "2026-10-01T08:00:00Z" });
const item = (over: Partial<FeedbackItem> = {}): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "needs_info", needsInput: true, underReview: false,
  replies: [reply(1, "maintainer"), reply(2, "user")], unreadReplies: 0, createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});
const PROFILE: FeedbackProfile = {
  level: 2, adoptedCount: 4, currentThreshold: 3, nextLevel: 3, nextThreshold: 6, remaining: 2, trustState: "active",
  trustExpiresAt: "2026-11-01T00:00:00Z", observedAt: "2026-10-07T08:00:00Z", effectiveLimits: { reportsPerHour: 6, reportsPerDay: 20, repliesPerHour: 5 },
};

async function drawn(items: FeedbackItem[], profile: FeedbackProfile | null) {
  const port = new MockPort() as unknown as AgentPort;
  const mine: Mine = { items, offline: false, unread: 0, hasNew: false, profile };
  port.myFeedback = vi.fn(async () => mine);
  port.feedbackSeen = vi.fn(async () => {});
  render(<FeedbackMine port={port} onFile={() => {}} />);
  await screen.findAllByText("FB-AAAA-0001");
}

const row = (author: string) => document.querySelector(`.fbk-thread li[data-author="${author}"]`) as HTMLElement;

describe("the level badge in a thread", () => {
  it("sits beside the reporter's own label and never on a maintainer reply", async () => {
    await drawn([item()], PROFILE);
    expect(row("user").querySelector('.lvl-badge[data-level="2"]')).toBeTruthy();
    expect(row("maintainer").querySelector(".lvl-badge")).toBeNull();
  });

  it("says the level in words for a reader who cannot see the badge", async () => {
    await drawn([item()], PROFILE);
    expect(row("user").textContent).toContain("等级 2");
  });

  it("is absent with no profile", async () => {
    await drawn([item()], null);
    expect(row("user").querySelector(".lvl-badge")).toBeNull();
  });
});
