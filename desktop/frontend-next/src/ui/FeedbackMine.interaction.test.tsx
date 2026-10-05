// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Feedback } from "./Feedback";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort } from "../port/port";
import { FEEDBACK_CODE, FEEDBACK_STATUSES, type FeedbackItem, type FeedbackMine as Mine } from "../port/feedback";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const item = (over: Partial<FeedbackItem>): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "received", needsInput: false, replies: [], unreadReplies: 0, createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});

function portWith(answer: Mine | Error) {
  const port = new MockPort() as unknown as AgentPort;
  port.myFeedback = vi.fn(async () => {
    if (answer instanceof Error) throw answer;
    return answer;
  });
  port.openExternal = vi.fn(async () => {});
  return port;
}

const row = (receipt: string) => screen.getByText(receipt).closest("li")!;

describe("my feedback", () => {
  it("draws one row per status, each with its own chip and timeline", async () => {
    const port = new MockPort() as unknown as AgentPort;
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-7K3M-9QX2");
    const shown = new Set([...document.querySelectorAll(".fbk-item:not([data-stale])")].map((li) => li.getAttribute("data-status")));
    expect([...shown].sort()).toEqual([...FEEDBACK_STATUSES].sort());

    const fixed = row("FB-7K3M-9QX2");
    expect(within(fixed).getByText("已修复")).toBeTruthy();
    expect(within(fixed).getByText("已在 v2.21.0 修复")).toBeTruthy();
    expect(within(fixed).getByRole("link", { name: "#11350" })).toBeTruthy();

    expect(within(row("FB-3F6G-2KV9")).getByText("已修复，将随下个版本发布")).toBeTruthy();
    expect(within(row("FB-8Q2J-6PW4")).getByText("不予修复", { selector: "span:not(.fbk-chip)" })).toBeTruthy();
    expect(within(row("FB-4T7V-3ZH1")).getByRole("link", { name: "#11299" })).toBeTruthy();
    expect(row("FB-4T7V-3ZH1").textContent).toContain("与 #11299 重复");

    const inProgress = row("FB-5N1C-8RT3").querySelector(".fbk-tl")!;
    expect(inProgress.querySelector('[aria-current="step"]')!.textContent).toBe("当前：处理中");
    const received = row("FB-9B4D-1XM6").querySelector(".fbk-tl")!;
    expect(received.querySelector('[aria-current="step"]')!.textContent).toBe("当前：已收到");
    expect(received.querySelectorAll('[data-state="todo"]')).toHaveLength(3);
  });

  it("names every status through its label, not through colour alone", async () => {
    const port = portWith({ offline: false, unread: 0, hasNew: false, items: FEEDBACK_STATUSES.map((status, i) => item({ receipt: `FB-AAAA-000${i}`, status })) });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0000");
    for (const [status, label] of [["received", "已收到"], ["needs_info", "需要补充信息"], ["answered", "维护者已回复"], ["closed", "已关闭此反馈"], ["recorded", "已登记"], ["in_progress", "处理中"], ["fixed", "已修复"], ["wontfix", "不予修复"], ["duplicate", "重复"]]) {
      expect(document.querySelector(`.fbk-chip[data-status="${status}"]`)!.textContent).toBe(label);
    }
  });

  it("opens a recorded issue through the port, not by navigating", async () => {
    const port = new MockPort() as unknown as AgentPort;
    port.openExternal = vi.fn(async () => {});
    const onFile = vi.fn();
    render(<FeedbackMine port={port} onFile={onFile} />);
    await userEvent.click(await screen.findByRole("link", { name: "#11302" }));
    expect(onFile).toHaveBeenCalledWith("https://github.com/esengine/DeepSeek-Reasonix/issues/11302");
  });

  it("says so when nothing was ever sent", async () => {
    render(<FeedbackMine port={portWith({ items: [], offline: false, unread: 0, hasNew: false })} onFile={() => {}} />);
    expect(await screen.findByText(/还没有提交过反馈/)).toBeTruthy();
    expect(document.querySelector(".fbk-list")).toBeNull();
  });

  it("shows a loading line first, then an error with a retry that works", async () => {
    let fail = true;
    const port = new MockPort() as unknown as AgentPort;
    port.myFeedback = vi.fn(async () => {
      if (fail) throw new HttpError(502, "x", { code: FEEDBACK_CODE.unavailable, error: "x" });
      return { items: [item({})], offline: false, unread: 0, hasNew: false };
    });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    expect(screen.getByText("正在读取你的反馈…")).toBeTruthy();
    expect((await screen.findByRole("alert")).textContent).toMatch(/暂时出了问题/);
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByText("FB-AAAA-0001")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("keeps the remembered list under a gentle banner when the service is offline", async () => {
    render(<FeedbackMine port={portWith({ items: [item({})], offline: true, unread: 0, hasNew: false })} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    const banner = screen.getAllByRole("status").find((n) => /连不上反馈服务/.test(n.textContent ?? ""))!;
    expect(banner.textContent).toMatch(/连不上反馈服务/);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("dims a row whose status can no longer be read and says so once", async () => {
    render(<FeedbackMine port={portWith({ items: [item({ statusUnavailable: true }), item({ receipt: "FB-AAAA-0002", statusUnavailable: true })], offline: false, unread: 0, hasNew: false })} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(screen.getAllByText(/已经查不到它们的最新状态/)).toHaveLength(1);
    expect(screen.getAllByText("状态已无法追踪")).toHaveLength(2);
    expect(document.querySelector(".fbk-tl")).toBeNull();
    expect(row("FB-AAAA-0001").getAttribute("data-stale")).toBe("");
  });
});

describe("refreshing", () => {
  it("reads once on open, again on Refresh and on re-opening the tab, and never on a timer", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const spy = vi.spyOn(port, "myFeedback");
    render(<Feedback port={port} tab="mine" onClose={() => {}} onError={() => {}} />);
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    await screen.findByText("FB-7K3M-9QX2");
    vi.useFakeTimers({ toFake: ["setInterval", "setTimeout"] });
    await vi.advanceTimersByTimeAsync(30 * 60_000);
    expect(spy).toHaveBeenCalledTimes(1);
    vi.useRealTimers();

    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));
    await userEvent.click(screen.getByRole("tab", { name: "发送反馈" }));
    await userEvent.click(screen.getByRole("tab", { name: "我的反馈" }));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(3));
  });

  it("does not read the list while the form tab is the one showing", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const spy = vi.spyOn(port, "myFeedback");
    render(<Feedback port={port} tab="send" onClose={() => {}} onError={() => {}} />);
    await screen.findByText("随反馈一起发送的环境信息");
    expect(spy).not.toHaveBeenCalled();
  });

  it("builds every issue link from the number alone and ignores the server's URL", async () => {
    const onFile = vi.fn();
    const port = portWith({ offline: false, unread: 0, hasNew: false, items: [item({ status: "recorded", issueNumber: 11350, issueUrl: "https://evil.example/phish?x=1" }), item({ receipt: "FB-AAAA-0002", status: "duplicate", issueNumber: 1, duplicateOf: 11302, issueUrl: "javascript:alert(1)" })] });
    render(<FeedbackMine port={port} onFile={onFile} />);
    await screen.findByText("FB-AAAA-0001");
    const links = [...document.querySelectorAll<HTMLAnchorElement>("a[data-action='feedback.link']")];
    expect(links.map((a) => a.href)).toEqual([
      "https://github.com/esengine/DeepSeek-Reasonix/issues/11350",
      "https://github.com/esengine/DeepSeek-Reasonix/issues/1",
      "https://github.com/esengine/DeepSeek-Reasonix/issues/11302",
    ]);
    for (const a of links) expect(a.rel).toBe("noopener noreferrer");
    expect(document.body.innerHTML).not.toContain("evil.example");
    await userEvent.click(links[0]!);
    expect(onFile).toHaveBeenCalledWith("https://github.com/esengine/DeepSeek-Reasonix/issues/11350");
  });

  it("does not link an issue number that is not a positive integer", async () => {
    const port = portWith({ offline: false, unread: 0, hasNew: false, items: [item({ status: "recorded", issueNumber: -3 }), item({ receipt: "FB-AAAA-0002", status: "recorded", issueNumber: 1.5 })] });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(document.querySelectorAll("a[data-action='feedback.link']")).toHaveLength(0);
  });

  it("says each timeline step's state in words as well as by its dot", async () => {
    const port = portWith({ offline: false, unread: 0, hasNew: false, items: [item({ status: "in_progress", issueNumber: 11377 })] });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    const steps = [...document.querySelectorAll(".fbk-tl li")].map((li) => [li.getAttribute("data-state"), li.querySelector(".sr-only")?.textContent]);
    expect(steps).toEqual([["done", "已完成："], ["done", "已完成："], ["current", "当前："], ["todo", "尚未开始："]]);
  });

  it("marks a snippet the server cut at 80 characters and leaves shorter ones alone", async () => {
    const cut = "x".repeat(80);
    const port = portWith({ offline: false, unread: 0, hasNew: false, items: [item({ titleSnippet: cut }), item({ receipt: "FB-AAAA-0002", titleSnippet: "short" })] });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(row("FB-AAAA-0001").querySelector(".fbk-snippet")!.textContent).toBe(cut + "…");
    expect(row("FB-AAAA-0002").querySelector(".fbk-snippet")!.textContent).toBe("short");
  });
});
