// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { draftKey } from "./drafts";
import { RunningMessageSetting } from "./RunningMessageSetting";
import { setRunningMessageIntent } from "../state/prefs";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const BODY = "Review the sample changes";
const rt = { id: "p1", root: "/sample", name: "Sample" } as RuntimeView;
const props = {
  rt, title: "Sample", active: true, visible: true, sideHost: null, side: false,
  onFocus() {}, onReport() {}, onSessionChanged() {}, pulse: 0, findPulse: 0,
  onSettings() {}, needsProject: false, onOpenProject() {}, onKeepHere() {},
  theme: "dark", dockW: 560, dockMax: 880, onDockW() {},
};
const box = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;
const type = (text: string) => fireEvent.change(box(), { target: { value: text, selectionStart: text.length } });

async function open(port = new MockPort()) {
  const status = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...status, running: true });
  vi.spyOn(port, "cancelQueued").mockResolvedValue();
  render(<Pane {...props} port={port} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "停下" })).toBeTruthy());
  return port;
}

describe("queued submission and withdrawal", () => {
  it("changes running submission from steering to a follow-up through the shared setting", async () => {
    const port = await open();
    const steer = vi.spyOn(port, "steer");
    const followup = vi.spyOn(port, "queueFollowup");
    render(<RunningMessageSetting />);
    fireEvent.click(screen.getByRole("radio", { name: "整轮结束后发送" }));
    type(BODY);
    expect(screen.getByText("Enter 排队 · Shift+Enter 换行")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "排队发送" }));
    await screen.findByRole("button", { name: "撤回" });
    expect(followup).toHaveBeenCalledWith(BODY, undefined);
    expect(steer).not.toHaveBeenCalled();
    expect((await port.queue()).items[0].intent).toBe("followup");
    fireEvent.click(screen.getByRole("button", { name: "撤回" }));
    await waitFor(() => expect(box().value).toBe(BODY));
    fireEvent.click(screen.getByRole("radio", { name: "下个工具边界插话" }));
    expect(screen.getByRole("button", { name: "插话" })).toBeTruthy();
  });

  it("holds a preferred follow-up while paused and restores it after reload", async () => {
    setRunningMessageIntent("followup");
    const port = new MockPort();
    await port.setQueuePaused(true);
    await open(port);
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "排队发送" }));
    await screen.findByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送");
    expect(screen.queryByRole("button", { name: "立即发送" })).toBeNull();
    cleanup();
    render(<Pane {...props} port={port} />);
    await screen.findByRole("button", { name: "撤回" });
    expect(box().value).toBe("");
    expect((await port.queue()).items[0].intent).toBe("followup");
    fireEvent.click(screen.getByRole("button", { name: "撤回" }));
    await waitFor(() => expect(box().value).toBe(BODY));
  });

  it.each(["", "A new draft", BODY])("clears on submit and restores once beside %j", async (draft) => {
    const port = await open();
    vi.spyOn(port, "steer").mockImplementation((text) => port.queueFollowup(text));
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await screen.findByRole("button", { name: "撤回" });
    expect(box().value).toBe("");
    type(draft);
    fireEvent.click(screen.getByRole("button", { name: "撤回" }));
    await waitFor(() => expect(box().value).toBe(draft && draft !== BODY ? `${draft}\n${BODY}` : BODY));
  });

  it("says the queue is paused when a line lands in a held queue, and says it once", async () => {
    const port = await open();
    vi.spyOn(port, "steer").mockImplementation(async (text) => ({ ...(await port.queueFollowup(text)), paused: true }));
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await screen.findByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送");
    expect(box().value).toBe("");
    type("Second");
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(port.steer).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(screen.getAllByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送")).toHaveLength(1);
  });

  it("takes the held-queue notice back once the queue resumes", async () => {
    const port = await open();
    vi.spyOn(port, "steer").mockImplementation(async (text) => ({ ...(await port.queueFollowup(text)), paused: true }));
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await screen.findByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送");
    await act(async () => { await port.setQueuePaused(true); });
    await act(async () => { await port.setQueuePaused(false); });
    await waitFor(() => expect(screen.queryByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送")).toBeNull());
  });

  it("says the queue is paused when an idle skill send is held by it", async () => {
    const port = new MockPort();
    render(<Pane {...props} port={port} />);
    vi.spyOn(port, "submit").mockResolvedValue({ itemId: "chip-1", disposition: "queued_followup", paused: true });
    await waitFor(() => expect(box()).toBeTruthy());
    type("先看一下 /in");
    await screen.findByRole("option", { name: /init/ });
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(box().value).toBe("先看一下 /init "));
    type("先看一下 /init 再说");
    fireEvent.keyDown(box(), { key: "Enter" });
    await screen.findByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送");
    expect(box().value).toBe("");
  });

  it("names a running send with a skill card as queued, because only plain text can steer", async () => {
    const port = await open();
    const steer = vi.spyOn(port, "steer");
    const followup = vi.spyOn(port, "queueFollowup");
    type("先看一下 /in");
    await screen.findByRole("option", { name: /init/ });
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(box().value).toBe("先看一下 /init "));
    type("先看一下 /init 再说");
    expect(screen.queryByRole("button", { name: "插话" })).toBeNull();
    expect(screen.getByText("Enter 排队 · Shift+Enter 换行")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "排队发送" }));
    await waitFor(() => expect(followup).toHaveBeenCalled());
    expect(steer).not.toHaveBeenCalled();
  });

  it("stays quiet when the queue is not paused", async () => {
    const port = await open();
    vi.spyOn(port, "steer").mockResolvedValue({ itemId: "steer-1", disposition: "steer_accepted" });
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(port.steer).toHaveBeenCalled());
    await act(async () => {});
    expect(screen.queryByText("待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送")).toBeNull();
  });

  it("keeps accepted steering out of the composer", async () => {
    const port = await open();
    vi.spyOn(port, "steer").mockResolvedValue({ itemId: "steer-1", disposition: "steer_accepted" });
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(port.steer).toHaveBeenCalledWith(BODY));
    await act(async () => {});
    expect(box().value).toBe("");
  });

  it("restores a refused submission once", async () => {
    const port = await open();
    vi.spyOn(port, "steer").mockRejectedValue(new HttpError(409, "refused", { code: "inbox.capacity_items" }));
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    await waitFor(() => expect(box().value).toBe(BODY));
    expect(screen.queryByRole("button", { name: "撤回" })).toBeNull();
  });

  it("pulls the queue after reload without filling the composer", async () => {
    const port = new MockPort();
    await port.queueFollowup(BODY);
    await open(port);
    await screen.findByRole("button", { name: "撤回" });
    expect(box().value).toBe("");
    cleanup();
    render(<Pane {...props} port={port} />);
    await screen.findByRole("button", { name: "撤回" });
    expect(box().value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "撤回" }));
    await waitFor(() => expect(box().value).toBe(BODY));
  });

  it("does not replay a withdrawal when the composer remounts", () => {
    const p = { port: new MockPort(), status: null, running: false, onSubmit: async () => true, onChanged() {}, onError() {} };
    const view = render(<Composer {...p} restore={{ n: 1, text: BODY }} />);
    expect(box().value).toBe("");
    view.rerender(<Composer {...p} restore={{ n: 2, text: BODY }} />);
    expect(box().value).toBe(BODY);
  });

  it("keeps the composer empty when draft identity settles during submission", async () => {
    const key = draftKey("", "/sample", "/sessions/sample.jsonl");
    localStorage.setItem(key, BODY);
    let accept!: (sent: boolean) => void;
    const pending = new Promise<boolean>((resolve) => { accept = resolve; });
    const p = { port: new MockPort(), status: null, running: true, onSubmit: () => pending, onChanged() {}, onError() {} };
    const view = render(<Composer {...p} draftKey="" />);
    type(BODY);
    fireEvent.click(screen.getByRole("button", { name: "插话" }));
    expect(box().value).toBe("");
    view.rerender(<Composer {...p} draftKey={key} />);
    expect(box().value).toBe("");
    await act(async () => accept(true));
    expect(box().value).toBe("");
    window.dispatchEvent(new Event("pagehide"));
    expect(localStorage.getItem(key)).toBeNull();
  });
});
