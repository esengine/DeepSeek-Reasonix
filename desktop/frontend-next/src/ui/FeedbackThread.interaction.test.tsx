// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { FeedbackMine } from "./FeedbackMine";
import { MockPort } from "../port/mock";
import { HttpError, type AgentPort } from "../port/port";
import { FEEDBACK_CODE, type FeedbackItem, type FeedbackMine as Mine, type FeedbackReply } from "../port/feedback";

afterEach(cleanup);

const reply = (id: number, author: FeedbackReply["author"], body: string): FeedbackReply => ({ id, author, body, createdAt: "2026-10-01T08:00:00Z" });

const item = (over: Partial<FeedbackItem>): FeedbackItem => ({
  receipt: "FB-AAAA-0001", category: "bug", titleSnippet: "snippet", status: "received", needsInput: false, replies: [], unreadReplies: 0,
  createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-21T00:00:00Z", ...over,
});

const mine = (items: FeedbackItem[], offline = false): Mine => ({ items, offline, unread: items.filter((i) => i.needsInput || i.unreadReplies > 0).length, hasNew: false });

function portWith(answer: () => Mine) {
  const port = new MockPort() as unknown as AgentPort;
  port.myFeedback = vi.fn(async () => answer());
  port.feedbackSeen = vi.fn(async () => {});
  port.replyFeedback = vi.fn(async () => ({ replyId: 9, createdAt: "2026-10-02T00:00:00Z" }));
  port.openExternal = vi.fn(async () => {});
  return port;
}

const row = (receipt: string) => screen.getByText(receipt).closest("li")!;
const box = () => screen.getByRole<HTMLTextAreaElement>("textbox", { name: /回复/ });
const sendButton = () => screen.getByRole<HTMLButtonElement>("button", { name: "发送回复" });

describe("the thread under a report", () => {
  it("draws maintainer and user messages in order, as text and nothing else", async () => {
    const hostile = '<img src=x onerror="alert(1)"> https://evil.example/phish [link](https://evil.example) **bold**';
    const port = portWith(() => mine([item({ status: "answered", replies: [reply(1, "maintainer", hostile), reply(2, "user", "thanks\nsecond line")] })]));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    const msgs = [...row("FB-AAAA-0001").querySelectorAll(".fbk-thread li")];
    expect(msgs.map((li) => li.getAttribute("data-author"))).toEqual(["maintainer", "user"]);
    expect(msgs[0]!.querySelector(".fbk-msg")!.textContent).toBe(hostile);
    expect(msgs[1]!.querySelector(".fbk-msg")!.textContent).toBe("thanks\nsecond line");
    const thread = row("FB-AAAA-0001").querySelector(".fbk-thread")!;
    expect(thread.querySelector("img, a, strong, b + a")).toBeNull();
    expect(document.body.innerHTML).not.toContain("<img src=x");
    expect(within(msgs[0] as HTMLElement).getByText("维护者")).toBeTruthy();
    expect(within(msgs[1] as HTMLElement).getByText("你")).toBeTruthy();
  });

  it("shows the last four messages and reveals the rest on request", async () => {
    const replies = Array.from({ length: 7 }, (_, i) => reply(i + 1, i % 2 ? "user" : "maintainer", `message ${i + 1}`));
    render(<FeedbackMine port={portWith(() => mine([item({ status: "answered", replies })]))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(screen.queryByText("message 3")).toBeNull();
    expect(screen.getByText("message 4")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "显示更早的 3 条" }));
    expect(screen.getByText("message 1")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /显示更早/ })).toBeNull();
  });
});

describe("a question waiting for the reporter", () => {
  const asking = () => mine([item({ status: "needs_info", needsInput: true, replies: [reply(5, "maintainer", "Which OS?")], unreadReplies: 1 })]);

  it("carries a needs-your-input chip and an open reply box, without any click", async () => {
    render(<FeedbackMine port={portWith(asking)} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(row("FB-AAAA-0001").querySelector(".fbk-chip")!.textContent).toBe("需要你回复");
    expect(box()).toBeTruthy();
    expect(sendButton().disabled).toBe(true);
    expect(screen.getByText("0 / 4,096 字节")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  });

  it("sends the trimmed reply once, clears the box and reads the list again", async () => {
    let answered = false;
    const port = portWith(() => (answered ? mine([item({ status: "received", replies: [reply(5, "maintainer", "Which OS?"), reply(9, "user", "macOS 15")] })]) : asking()));
    port.replyFeedback = vi.fn(async () => {
      answered = true;
      return { replyId: 9, createdAt: "2026-10-02T00:00:00Z" };
    });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.type(box(), "  macOS 15  ");
    await userEvent.click(sendButton());
    await waitFor(() => expect(port.replyFeedback).toHaveBeenCalledTimes(1));
    expect(port.replyFeedback).toHaveBeenCalledWith("FB-AAAA-0001", "macOS 15");
    expect(await screen.findByText("macOS 15")).toBeTruthy();
    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
    expect(row("FB-AAAA-0001").querySelector(".fbk-chip")!.textContent).toBe("已收到");
    expect(port.myFeedback).toHaveBeenCalledTimes(2);
  });

  it("counts UTF-8 bytes against the limit and holds Send back past it", async () => {
    render(<FeedbackMine port={portWith(asking)} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(box());
    await userEvent.paste("你".repeat(1366));
    expect(screen.getByText("4,098 / 4,096 字节")).toBeTruthy();
    expect(box().getAttribute("aria-invalid")).toBe("true");
    expect(sendButton().disabled).toBe(true);
  });

  it.each([
    [FEEDBACK_CODE.replyLimit, /回复次数已到上限/],
    [FEEDBACK_CODE.notReplyable, /现在不接收回复/],
    [FEEDBACK_CODE.rateLimited, /提交得太频繁/],
    [FEEDBACK_CODE.badToken, /身份已经变了/],
    [FEEDBACK_CODE.offline, /你写的回复还在/],
    [FEEDBACK_CODE.unavailable, /你写的回复还在/],
    [FEEDBACK_CODE.disabled, /暂时关闭/],
  ])("says what %s means and keeps the text", async (code, words) => {
    const port = portWith(asking);
    port.replyFeedback = vi.fn(async () => {
      throw new HttpError(409, code, { code, error: code });
    });
    const onFile = vi.fn();
    render(<FeedbackMine port={port} onFile={onFile} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.type(box(), "macOS 15");
    await userEvent.click(sendButton());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(words);
    expect(box().value).toBe("macOS 15");
    if (code === FEEDBACK_CODE.unavailable) {
      expect(alert.textContent).toContain("重发前先刷新列表确认是否送达");
      await userEvent.click(screen.getByRole("button", { name: "去 GitHub" }));
      expect(onFile).toHaveBeenCalledWith("https://github.com/esengine/DeepSeek-Reasonix/issues/new/choose");
    }
    await waitFor(() => expect(port.myFeedback).toHaveBeenCalledTimes(code === FEEDBACK_CODE.notReplyable ? 2 : 1));
  });

  it("names the field when the kernel refuses the text itself", async () => {
    const port = portWith(asking);
    port.replyFeedback = vi.fn(async () => {
      throw new HttpError(400, "invalid", { code: FEEDBACK_CODE.invalid, error: "x", params: { field: "body", reason: "too_long" } });
    });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.type(box(), "x");
    await userEvent.click(sendButton());
    expect((await screen.findByRole("alert")).textContent).toBe("回复太长了，请缩短后再发。");
  });
});

describe("the box after a reply", () => {
  it("starts empty the next time it is opened", async () => {
    const port = portWith(() => mine([item({ status: "answered", replies: [reply(1, "maintainer", "done")] })]));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(screen.getByRole("button", { name: "回复" }));
    await userEvent.type(box(), "thanks");
    await userEvent.click(sendButton());
    await waitFor(() => expect(port.replyFeedback).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
    await userEvent.click(screen.getByRole("button", { name: "回复" }));
    expect(box().value).toBe("");
  });
});

describe("the timeline of a report that never reaches GitHub", () => {
  it("is the receipt and what became of it, and nothing about issues", async () => {
    const items = [
      item({ receipt: "FB-AAAA-0001", status: "closed" }),
      item({ receipt: "FB-AAAA-0002", status: "answered", replies: [reply(1, "maintainer", "done")] }),
      item({ receipt: "FB-AAAA-0003", status: "needs_info", needsInput: true, replies: [reply(2, "maintainer", "q")] }),
    ];
    render(<FeedbackMine port={portWith(() => mine(items))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    const steps = (receipt: string) => [...row(receipt).querySelectorAll(".fbk-tl li")].map((li) => [li.getAttribute("data-state"), li.textContent!.replace(/^[^：]*：/, "")]);
    expect(steps("FB-AAAA-0001")).toEqual([["done", "已收到"], ["done", "已关闭此反馈"]]);
    expect(steps("FB-AAAA-0002")).toEqual([["done", "已收到"], ["done", "维护者已回复"]]);
    expect(steps("FB-AAAA-0003")).toEqual([["done", "已收到"], ["current", "等你补充信息"]]);
  });
});

describe("who may reply", () => {
  it("offers a Reply button on answered, recorded and in-progress reports, and opens the box from it", async () => {
    const items = (["answered", "recorded", "in_progress"] as const).map((status, i) => item({ receipt: `FB-AAAA-000${i + 1}`, status, replies: status === "answered" ? [reply(1, "maintainer", "done")] : [] }));
    render(<FeedbackMine port={portWith(() => mine(items))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(screen.getAllByRole("button", { name: "回复" })).toHaveLength(3);
    expect(screen.queryByRole("textbox")).toBeNull();
    await userEvent.click(within(row("FB-AAAA-0002")).getByRole("button", { name: "回复" }));
    expect(within(row("FB-AAAA-0002")).getByRole("textbox")).toBeTruthy();
    await userEvent.click(within(row("FB-AAAA-0002")).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("offers nothing on received, fixed, wont-fix, duplicate, closed and unreadable reports", async () => {
    const items = (["received", "fixed", "wontfix", "duplicate", "closed"] as const).map((status, i) => item({ receipt: `FB-AAAA-000${i + 1}`, status }));
    items.push(item({ receipt: "FB-AAAA-0009", status: "needs_info", needsInput: true, statusUnavailable: true }));
    render(<FeedbackMine port={portWith(() => mine(items))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(screen.queryByRole("button", { name: "回复" })).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("says replies wait for the network when the list is the offline copy", async () => {
    render(<FeedbackMine port={portWith(() => mine([item({ status: "needs_info", needsInput: true })], true))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("离线时暂时不能回复。")).toBeTruthy();
  });

  it("warns that a reply on a report with a public issue is copied there, and only then", async () => {
    const items = [item({ status: "recorded", issueNumber: 11350 }), item({ receipt: "FB-AAAA-0002", status: "answered" })];
    render(<FeedbackMine port={portWith(() => mine(items))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(within(row("FB-AAAA-0001")).getByRole("button", { name: "回复" }));
    await userEvent.click(within(row("FB-AAAA-0002")).getByRole("button", { name: "回复" }));
    expect(within(row("FB-AAAA-0001")).getByText(/公开议题 #11350.*公开转发/)).toBeTruthy();
    expect(within(row("FB-AAAA-0002")).getByText(/只发给维护者，不会公开/)).toBeTruthy();
    expect(within(row("FB-AAAA-0002")).queryByText(/公开转发/)).toBeNull();
  });
});

describe("reading the replies", () => {
  it("marks each report with new replies seen, keeps its New mark for this visit, and reports what still waits", async () => {
    const items = [
      item({ receipt: "FB-AAAA-0001", status: "answered", unreadReplies: 1, replies: [reply(1, "maintainer", "old"), reply(2, "user", "ok"), reply(3, "maintainer", "fresh")] }),
      item({ receipt: "FB-AAAA-0002", status: "needs_info", needsInput: true, replies: [reply(4, "maintainer", "q")] }),
      item({ receipt: "FB-AAAA-0003", status: "answered", replies: [reply(6, "maintainer", "read long ago")] }),
    ];
    const port = portWith(() => mine(items));
    const onUnread = vi.fn();
    render(<FeedbackMine port={port} onFile={() => {}} onUnread={onUnread} />);
    await screen.findByText("FB-AAAA-0001");
    await waitFor(() => expect(onUnread).toHaveBeenCalledWith(1));
    expect(port.feedbackSeen).toHaveBeenCalledTimes(1);
    expect(port.feedbackSeen).toHaveBeenCalledWith("FB-AAAA-0001", 3);
    const marks = [...document.querySelectorAll(".fbk-new")];
    expect(marks).toHaveLength(1);
    expect(marks[0]!.closest("li")!.querySelector(".fbk-msg")!.textContent).toBe("fresh");
  });

  it("keeps a report counted when the kernel could not record that it was seen", async () => {
    const items = [item({ status: "answered", unreadReplies: 1, replies: [reply(1, "maintainer", "hi")] })];
    const port = portWith(() => mine(items));
    port.feedbackSeen = vi.fn(async () => {
      throw new Error("disk");
    });
    const onUnread = vi.fn();
    render(<FeedbackMine port={port} onFile={() => {}} onUnread={onUnread} />);
    await waitFor(() => expect(onUnread).toHaveBeenCalledWith(1));
  });

  it("never lets a later, smaller count shrink the marks of this visit", async () => {
    let unread = 2;
    const replies = [reply(1, "maintainer", "a"), reply(2, "maintainer", "b"), reply(3, "user", "c")];
    const port = portWith(() => mine([item({ status: "answered", unreadReplies: unread, replies })]));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(document.querySelectorAll(".fbk-new")).toHaveLength(2);
    unread = 1;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(port.myFeedback).toHaveBeenCalledTimes(2));
    expect(document.querySelectorAll(".fbk-new")).toHaveLength(2);
  });

  it("keeps the New marks through a refresh that finds nothing newer", async () => {
    let unread = 1;
    const port = portWith(() => mine([item({ status: "answered", unreadReplies: unread, replies: [reply(1, "maintainer", "hi")] })]));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(document.querySelectorAll(".fbk-new")).toHaveLength(1);
    unread = 0;
    await userEvent.click(screen.getByRole("button", { name: "刷新列表" }));
    await waitFor(() => expect(port.myFeedback).toHaveBeenCalledTimes(2));
    expect(document.querySelectorAll(".fbk-new")).toHaveLength(1);
  });
});

describe("review fixes", () => {
  it("counts a report once when it both needs input and could not be marked seen", async () => {
    const items = [item({ status: "needs_info", needsInput: true, unreadReplies: 1, replies: [reply(1, "maintainer", "q")] })];
    const port = portWith(() => mine(items));
    port.feedbackSeen = vi.fn(async () => { throw new Error("disk"); });
    const onUnread = vi.fn();
    render(<FeedbackMine port={port} onFile={() => {}} onUnread={onUnread} />);
    await waitFor(() => expect(onUnread).toHaveBeenCalledWith(1));
    expect(onUnread).not.toHaveBeenCalledWith(2);
  });

  it("falls back to a readable limit when the environment could not be read", async () => {
    const port = portWith(() => mine([item({ status: "needs_info", needsInput: true })]));
    port.feedbackEnv = vi.fn(async () => { throw new Error("x"); });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.type(box(), "hi");
    expect(screen.getByText("2 / 4,096 字节")).toBeTruthy();
    expect(sendButton().disabled).toBe(false);
  });

  it("names each report for assistive tech and announces a sent reply politely", async () => {
    const port = portWith(() => mine([item({ status: "needs_info", needsInput: true })]));
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(row("FB-AAAA-0001").getAttribute("aria-label")).toBe("FB-AAAA-0001 snippet");
    await userEvent.type(box(), "hi");
    await userEvent.click(sendButton());
    await waitFor(() => expect(screen.getByRole("status").textContent).toBe("回复已发送。"));
  });

  it("sets reply text direction automatically and isolates it", async () => {
    render(<FeedbackMine port={portWith(() => mine([item({ status: "answered", replies: [reply(1, "maintainer", "مرحبا")] })]))} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    expect(document.querySelector(".fbk-msg")!.getAttribute("dir")).toBe("auto");
  });

  it("marks folded unread replies seen only when they are expanded", async () => {
    const replies = Array.from({ length: 6 }, (_, i) => reply(i + 1, "maintainer", `m${i + 1}`));
    const port = portWith(() => mine([item({ status: "answered", unreadReplies: 6, replies })]));
    const onUnread = vi.fn();
    render(<FeedbackMine port={port} onFile={() => {}} onUnread={onUnread} />);
    await screen.findByText("FB-AAAA-0001");
    await waitFor(() => expect(onUnread).toHaveBeenCalledWith(1));
    expect(port.feedbackSeen).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: /显示更早/ }));
    await waitFor(() => expect(port.feedbackSeen).toHaveBeenCalledWith("FB-AAAA-0001", 6));
    await waitFor(() => expect(onUnread).toHaveBeenLastCalledWith(0));
  });

  it("refreshes once after a not-replyable refusal and keeps the draft", async () => {
    const port = portWith(() => mine([item({ status: "answered", replies: [reply(1, "maintainer", "x")] })]));
    port.replyFeedback = vi.fn(async () => { throw new HttpError(409, "n", { code: FEEDBACK_CODE.notReplyable, error: "n" }); });
    render(<FeedbackMine port={port} onFile={() => {}} />);
    await screen.findByText("FB-AAAA-0001");
    await userEvent.click(screen.getByRole("button", { name: "回复" }));
    await userEvent.type(box(), "draft");
    await userEvent.click(sendButton());
    await waitFor(() => expect(port.myFeedback).toHaveBeenCalledTimes(2));
    expect(box().value).toBe("draft");
  });
});
