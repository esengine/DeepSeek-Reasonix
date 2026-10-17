// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { dropDraft } from "./feedbackdraft";
import { MockHub } from "../port/mock_hub";
import { HttpError } from "../port/port";
import { MockFeedback } from "../port/mock_feedback";
import type { FeedbackMine } from "../port/feedback";

afterEach(() => {
  cleanup();
  dropDraft();
  localStorage.removeItem("rx-show-session-tabs");
  vi.restoreAllMocks();
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
  const port = h.machinePort();
  const asked = vi.fn();
  const mine = port.myFeedback.bind(port);
  port.myFeedback = vi.fn(async () => {
    asked();
    return unread(await mine());
  });
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
    for (const open of screen.getAllByRole("button", { name: /展开对话/ })) await userEvent.click(open);
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

  it("is read when the app opens, then re-armed only every ten minutes because a report is open", async () => {
    const { hub: h, asked } = quiet();
    const long: number[] = [];
    const wrap = (name: "setInterval" | "setTimeout") => {
      const real = window[name].bind(window) as (fn: TimerHandler, ms?: number, ...a: unknown[]) => number;
      return vi.spyOn(window, name).mockImplementation(((fn: TimerHandler, ms?: number, ...a: unknown[]) => {
        if ((ms ?? 0) >= 10_000 && !new Error().stack?.includes("@testing-library") && !new Error().stack?.includes("updatewatch")) long.push(ms!);
        return real(fn, ms, ...a);
      }) as typeof window.setTimeout);
    };
    const spies = [wrap("setInterval"), wrap("setTimeout")];
    render(<App hub={h} />);
    await screen.findByRole("button", { name: /3 项待查看/ });
    expect(asked).toHaveBeenCalledTimes(1);
    spies.forEach((s) => s.mockRestore());
    expect(long).toEqual([600_000]);
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


it("starts and rebinds feedback polling when the first local owner becomes available", async () => {
  localStorage.setItem("rx-show-session-tabs", "on");
  const h = hub();
  await h.close("r1");
  const remote = await h.openRemote({ host: "gpu", workspace: "/remote", sessionPath: "/remote/session.jsonl" });
  const remoteReads = vi.spyOn(h.portFor(remote), "myFeedback");
  const machine = h.machinePort();
  const original = machine.myFeedback.bind(machine);
  const read = vi.spyOn(machine, "myFeedback").mockImplementation(async () => {
    if (!(await h.runtimes()).some((rt) => !rt.host)) throw new HttpError(503, "local required", { code: "settings.local_runtime_required" });
    return original();
  });
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  render(<App hub={h} />);
  await screen.findByRole("button", { name: "发送反馈" });
  await userEvent.click(await screen.findByText("上一次的会话"));
  await screen.findByRole("button", { name: /发送反馈.*3 项待查看/ });
  const first = (await h.runtimes()).find((rt) => !rt.host)!;
  const firstReads = read.mock.calls.length;
  const project = await screen.findByRole("treeitem", { name: /my-website/ });
  if (project.getAttribute("aria-expanded") === "false") await userEvent.click(project);
  await userEvent.click(await screen.findByRole("treeitem", { name: /站点改版/ }));
  await waitFor(async () => expect((await h.runtimes()).filter((rt) => !rt.host)).toHaveLength(2));
  expect(read).toHaveBeenCalledTimes(firstReads);
  const tab = document.querySelector(`[data-pane="${first.id}"][role="tab"]`)! as HTMLElement;
  await userEvent.click(within(tab).getByRole("button", { name: "关闭这个面板" }));
  await waitFor(() => expect(read).toHaveBeenCalledTimes(firstReads + 1));
  expect(remoteReads).not.toHaveBeenCalled();
});
