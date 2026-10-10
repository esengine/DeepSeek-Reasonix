// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Feedback } from "./Feedback";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort } from "../port/port";
import { FEEDBACK_CODE, type FeedbackItem, type FeedbackMine as Mine, type FeedbackProfile } from "../port/feedback";
import { boot, STORAGE } from "../i18n";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const item = (over: Partial<FeedbackItem> = {}): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "fixed", resolvedVersion: "v2.25.0", needsInput: false, underReview: false, replies: [], unreadReplies: 0, createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});

const MID: FeedbackProfile = {
  level: 2, adoptedCount: 4, currentThreshold: 3, nextLevel: 3, nextThreshold: 6, remaining: 2, trustState: "active",
  trustExpiresAt: "2026-11-01T00:00:00Z", observedAt: "2026-10-07T08:00:00Z", effectiveLimits: { reportsPerHour: 6, reportsPerDay: 20, repliesPerHour: 5 },
};

const profile = (over: Partial<FeedbackProfile>): FeedbackProfile => ({ ...MID, ...over });

const mine = (p: FeedbackProfile | null, over: Partial<Mine> = {}): Mine => ({ items: [item()], offline: false, unread: 0, hasNew: false, profile: p, ...over });

function portWith(answer: () => Mine | Error) {
  const port = new MockPort() as unknown as AgentPort;
  port.myFeedback = vi.fn(async () => {
    const got = answer();
    if (got instanceof Error) throw got;
    return got;
  });
  port.openExternal = vi.fn(async () => {});
  return port;
}

async function shown(p: FeedbackProfile | null, over: Partial<Mine> = {}) {
  render(<FeedbackMine port={portWith(() => mine(p, over))} onFile={() => {}} />);
  await screen.findByText("FB-AAAA-0001");
}

const level = () => document.querySelector(".fbk-level");
const text = (sel: string) => document.querySelector(sel)?.textContent;

describe("the level row", () => {
  it("states the level, what shipped, what is left and the limits in force", async () => {
    await shown(MID);
    expect(level()?.getAttribute("data-level")).toBe("2");
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");
    expect(text(".fbk-level-progress")).toBe("已落地 4 条，再 2 条升到「小树」");
    expect(text(".fbk-level-limits")).toBe("每小时最多 6 条反馈 · 每天 20 条 · 每小时 5 条回复");
  });

  it("draws progress toward the next level as a track that says its own value", async () => {
    await shown(MID);
    const bar = screen.getByRole("progressbar");
    expect(bar.getAttribute("aria-valuemin")).toBe("0");
    expect(bar.getAttribute("aria-valuenow")).toBe("1");
    expect(bar.getAttribute("aria-valuemax")).toBe("3");
    expect(bar.getAttribute("aria-valuetext")).toBe("已落地 4 条，再 2 条升到「小树」");
    expect((bar.querySelector(".fbk-level-fill") as HTMLElement).style.width).toBe("33.33%");
  });

  it("shows the badge of the level reached and the next one locked, paired with words", async () => {
    await shown(MID);
    expect(document.querySelector('.fbk-level .lvl-badge[data-level="2"]')).toBeTruthy();
    expect(document.querySelector(".fbk-level .lvl-badge[data-locked]")?.getAttribute("data-level")).toBe("3");
    expect(text(".fbk-level-next")).toContain("小树");
  });

  it("starts at L0 with an empty track, not at nothing", async () => {
    await shown(profile({ level: 0, adoptedCount: 0, currentThreshold: 0, nextLevel: 1, nextThreshold: 1, remaining: 1, trustState: "none", trustExpiresAt: null, effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 } }));
    expect(text(".fbk-level-name")).toBe("L0 · 新种");
    expect(text(".fbk-level-progress")).toBe("已落地 0 条，再 1 条升到「萌芽」");
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("0");
    expect(text(".fbk-level-limits")).toBe("每小时最多 3 条反馈 · 每天 10 条 · 每小时 3 条回复");
    expect(document.querySelector(".fbk-level-note")).toBeNull();
  });

  it("has nothing left to climb at the top: no track, no locked badge", async () => {
    await shown(profile({ level: 6, adoptedCount: 50, currentThreshold: 48, nextLevel: null, nextThreshold: null, remaining: null, effectiveLimits: { reportsPerHour: 12, reportsPerDay: 60, repliesPerHour: 10 } }));
    expect(text(".fbk-level-name")).toBe("L6 · 共林");
    expect(text(".fbk-level-progress")).toBe("已落地 50 条，已是最高等级");
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(document.querySelector(".lvl-badge[data-locked]")).toBeNull();
  });

  it("reads a level this build has no name for without inventing one", async () => {
    await shown(profile({ level: 9, adoptedCount: 99, currentThreshold: 90, nextLevel: null, nextThreshold: null, remaining: null }));
    expect(text(".fbk-level-name")).toBe("L9");
    expect(document.querySelector(".fbk-level .lvl-badge")).toBeTruthy();
  });

  it("speaks English when the interface does", async () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    await shown(MID);
    expect(text(".fbk-level-name")).toBe("L2 · Seedling");
    expect(text(".fbk-level-progress")).toBe("4 shipped, 2 more to reach Sapling");
    expect(text(".fbk-level-limits")).toBe("Up to 6 reports an hour · 20 a day · 5 replies an hour");
    expect(screen.getByRole("progressbar").getAttribute("aria-label")).toBe("Progress to the next level");
  });
});

describe("why the limits are what they are", () => {
  it("tells a legacy trusted install its higher limits end on a date", async () => {
    await shown(profile({ level: 0, adoptedCount: 0, currentThreshold: 0, nextLevel: 1, nextThreshold: 1, remaining: 1, trustState: "legacy_active", effectiveLimits: { reportsPerHour: 12, reportsPerDay: 60, repliesPerHour: 10 } }));
    const note = text(".fbk-level-note")!;
    expect(note).toContain("较高的额度");
    expect(note).toContain(new Date("2026-11-01T00:00:00Z").toLocaleDateString("zh-CN", { year: "numeric", month: "short", day: "numeric" }));
    expect(text(".fbk-level-limits")).toContain("12 条反馈");
  });

  it("says a lapsed install keeps its level and count but not the higher limits", async () => {
    await shown(profile({ trustState: "lapsed", trustExpiresAt: null, effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 } }));
    expect(text(".fbk-level-note")).toContain("已经失效");
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");
    expect(text(".fbk-level-limits")).toContain("3 条反馈");
  });

  it("gives an active install the date its limits run to", async () => {
    await shown(MID);
    expect(text(".fbk-level-note")).toContain("有效期至");
  });

  it("says nothing more about a revoked install than that its limits are the standard ones", async () => {
    await shown(profile({ trustState: "revoked", trustExpiresAt: null, effectiveLimits: { reportsPerHour: 3, reportsPerDay: 10, repliesPerHour: 3 } }));
    expect(text(".fbk-level-note")).toBeUndefined();
    expect(text(".fbk-level-limits")).toContain("3 条反馈");
  });

  it("reads a trust state it does not know as raising nothing", async () => {
    await shown(profile({ trustState: "suspended" as FeedbackProfile["trustState"], trustExpiresAt: "2026-11-01T00:00:00Z" }));
    expect(text(".fbk-level-note")).toBeUndefined();
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");
  });
});

describe("when the standing cannot be confirmed", () => {
  it("labels the last confirmed level as stale and dates it while offline", async () => {
    await shown(MID, { offline: true });
    const stale = text(".fbk-level-stale")!;
    expect(stale).toContain("上次确认");
    expect(stale).toContain(new Date("2026-10-07T08:00:00Z").toLocaleDateString("zh-CN", { year: "numeric", month: "short", day: "numeric" }));
    expect(level()?.hasAttribute("data-stale")).toBe(true);
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");
  });

  it("says the level is unavailable when reports exist and the service stated none", async () => {
    await shown(null);
    expect(level()).toBeNull();
    expect(screen.getByText("等级暂时无法显示。")).toBeTruthy();
    expect(screen.getByText("FB-AAAA-0001")).toBeTruthy();
  });

  it("says nothing of levels to someone who has sent nothing", async () => {
    render(<FeedbackMine port={portWith(() => mine(null, { items: [] }))} onFile={() => {}} />);
    await screen.findByText(/还没有提交过反馈/);
    expect(level()).toBeNull();
    expect(screen.queryByText("等级暂时无法显示。")).toBeNull();
  });

  it("keeps the last standing under the error when a refresh fails", async () => {
    let fail = false;
    const port = portWith(() => (fail ? new HttpError(502, "x", { code: FEEDBACK_CODE.unavailable, error: "x" }) : mine(MID)));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    fail = true;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await screen.findByRole("alert");
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");
  });
});

describe("the row beside the rest of the panel", () => {
  it("is re-read with the list: Refresh and a sent reply both bring the new level", async () => {
    let now = MID;
    const port = portWith(() => mine(now, { items: [item({ status: "needs_info", needsInput: true, replies: [{ id: 5, author: "maintainer", body: "which OS?", createdAt: "2026-10-01T08:00:00Z" }] })] }));
    port.replyFeedback = vi.fn(async () => ({ replyId: 6, createdAt: "2026-10-07T08:00:00Z" }));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(text(".fbk-level-name")).toBe("L2 · 幼苗");

    now = profile({ level: 3, adoptedCount: 6, currentThreshold: 6, nextLevel: 4, nextThreshold: 12, remaining: 6 });
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(text(".fbk-level-name")).toBe("L3 · 小树"));

    now = profile({ level: 4, adoptedCount: 12, currentThreshold: 12, nextLevel: 5, nextThreshold: 24, remaining: 12 });
    await userEvent.type(screen.getByRole("textbox"), "macOS 15");
    await userEvent.click(screen.getByRole("button", { name: "发送回复" }));
    await waitFor(() => expect(text(".fbk-level-name")).toBe("L4 · 繁花"));
  });

  it("leaves the unread count, the offline banner and the rows exactly as they were", async () => {
    const unread: number[] = [];
    const port = portWith(() => mine(MID, { offline: true, items: [item({ receipt: "FB-AAAA-0002", status: "needs_info", needsInput: true })] }));
    render(<FeedbackMine port={port} onFile={() => {}} onUnread={(n) => unread.push(n)} />);
    await screen.findByText("FB-AAAA-0002");
    expect(unread.at(-1)).toBe(1);
    expect(screen.getAllByRole("status").some((n) => /连不上反馈服务/.test(n.textContent ?? ""))).toBe(true);
    expect(within(document.querySelector(".fbk-item")!).getByText("需要你回复")).toBeTruthy();
  });

  it("draws once the panel opens on My feedback and not while the form tab shows", async () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<Feedback port={port} tab="mine" onClose={() => {}} onError={() => {}} />);
    await screen.findByText("FB-7K3M-9QX2");
    expect(document.querySelectorAll(".fbk-level")).toHaveLength(1);
    cleanup();
    render(<Feedback port={port} tab="send" onClose={() => {}} onError={() => {}} />);
    await screen.findByText("随反馈一起发送的环境信息");
    expect(document.querySelector(".fbk-level")).toBeNull();
  });
});
