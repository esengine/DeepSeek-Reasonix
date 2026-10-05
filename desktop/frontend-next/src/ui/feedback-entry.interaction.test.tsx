// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { dropDraft } from "./feedbackdraft";
import { MockHub } from "../port/mock_hub";
import { MockFeedback } from "../port/mock_feedback";
import type { FeedbackMine } from "../port/feedback";

afterEach(() => {
  cleanup();
  dropDraft();
});

function hub() {
  const hub = new MockHub();
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return hub;
}

const nothingNew = () =>
  quiet((m) => ({ ...m, unread: 0, hasNew: false, items: m.items.map((i) => ({ ...i, needsInput: false, unreadReplies: 0 })) })).hub;

describe("reaching feedback", () => {
  it("opens from the rail and closes on Escape, leaving focus on the rail entry", async () => {
    render(<App hub={nothingNew()} />);
    const entry = await screen.findByRole("button", { name: "发送反馈" });
    await userEvent.click(entry);
    expect(await screen.findByRole("dialog", { name: "反馈" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "发送反馈", selected: true })).toBeTruthy();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "反馈" })).toBeNull();
    expect(document.activeElement).toBe(entry);
  });

  it("opens from the command palette, on the list for My feedback", async () => {
    render(<App hub={nothingNew()} />);
    await screen.findByRole("button", { name: "发送反馈" });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.keyboard("feedback");
    await userEvent.click(await screen.findByText("我的反馈"));
    expect(await screen.findByRole("tab", { name: "我的反馈", selected: true })).toBeTruthy();
    expect(await screen.findByText("FB-7K3M-9QX2")).toBeTruthy();
  });
});

function quiet(unread: (m: FeedbackMine) => FeedbackMine = (m) => m) {
  const h = hub();
  const build = h.portFor.bind(h);
  const asked = vi.fn();
  const wrapped = new WeakSet<object>();
  h.portFor = (rt) => {
    const port = build(rt);
    if (wrapped.has(port)) return port;
    wrapped.add(port);
    const mine = port.myFeedback.bind(port);
    port.myFeedback = vi.fn(async () => {
      asked();
      return unread(await mine());
    });
    return port;
  };
  return { hub: h, asked };
}

describe("the unread badge", () => {
  it("counts the reports wanting attention on the entry, and opens straight onto My feedback", async () => {
    const { hub: h } = quiet();
    render(<App hub={h} />);
    const entry = await screen.findByRole("button", { name: /发送反馈.*3 项待查看/ });
    expect(entry.querySelector(".fbk-badge")!.textContent).toBe("3");
    await userEvent.click(entry);
    expect(await screen.findByRole("tab", { name: "我的反馈", selected: true })).toBeTruthy();
    await screen.findByText("FB-1C5W-7NB2");
  });

  it("drops to what still waits for an answer once the replies were shown", async () => {
    const { hub: h } = quiet();
    render(<App hub={h} />);
    await userEvent.click(await screen.findByRole("button", { name: /3 项待查看/ }));
    await screen.findByText("FB-1C5W-7NB2");
    await waitFor(() => expect(screen.getByRole("button", { name: /发送反馈.*1 项待查看/ })).toBeTruthy());
    expect(document.querySelector(".studio-feedback .fbk-badge")!.textContent).toBe("1");
  });

  it("is absent when nothing is new, and then the entry opens the form", async () => {
    render(<App hub={nothingNew()} />);
    const entry = await screen.findByRole("button", { name: "发送反馈" });
    await waitFor(() => expect(document.querySelector(".fbk-badge")).toBeNull());
    await userEvent.click(entry);
    expect(await screen.findByRole("tab", { name: "发送反馈", selected: true })).toBeTruthy();
  });

  it("shows on the palette item too", async () => {
    const { hub: h } = quiet();
    render(<App hub={h} />);
    await screen.findByRole("button", { name: /3 项待查看/ });
    await userEvent.keyboard("{Control>}k{/Control}");
    await userEvent.keyboard("feedback");
    const hit = (await screen.findByText("我的反馈")).closest(".palette-hit")!;
    expect(hit.textContent).toContain("3 项待查看");
  });

  it("is read when the app opens and never on a timer", async () => {
    const { hub: h, asked } = quiet();
    const long: number[] = [];
    const wrap = (name: "setInterval" | "setTimeout") => {
      const real = window[name].bind(window) as (fn: TimerHandler, ms?: number, ...a: unknown[]) => number;
      return vi.spyOn(window, name).mockImplementation(((fn: TimerHandler, ms?: number, ...a: unknown[]) => {
        if ((ms ?? 0) >= 10_000) long.push(ms!);
        return real(fn, ms, ...a);
      }) as typeof window.setTimeout);
    };
    const spies = [wrap("setInterval"), wrap("setTimeout")];
    render(<App hub={h} />);
    await screen.findByRole("button", { name: /3 项待查看/ });
    expect(asked).toHaveBeenCalledTimes(1);
    spies.forEach((s) => s.mockRestore());
    expect(long).toEqual([]);
  });

  it("is not read again when another session is opened", async () => {
    const { hub: h, asked } = quiet();
    render(<App hub={h} />);
    await screen.findByRole("button", { name: /3 项待查看/ });
    await userEvent.click(await screen.findByText("上一次的会话"));
    await waitFor(() => expect(document.querySelector(".app")).toBeTruthy());
    expect(asked).toHaveBeenCalledTimes(1);
  });

  it("caps the number at 9+ and keeps the real count for the palette", async () => {
    const { hub: h } = quiet((m) => ({ ...m, unread: 12 }));
    render(<App hub={h} />);
    const entry = await screen.findByRole("button", { name: /12 项待查看/ });
    expect(entry.querySelector(".fbk-badge")!.textContent).toBe("9+");
  });
});

describe("the mock keeps what the badge reads", () => {
  it("counts a question and an unseen reply, and lets a reply clear the question", async () => {
    const port = new MockFeedback();
    expect((await port.myFeedback()).unread).toBe(3);
    await port.replyFeedback("FB-1C5W-7NB2", "macOS");
    await port.feedbackSeen("FB-8D3X-4LP6", 11);
    await port.feedbackSeen("FB-5N1C-8RT3", 22);
    expect((await port.myFeedback()).unread).toBe(0);
  });
});
