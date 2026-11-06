// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { SidePanel } from "./SidePanel";
import { MockPort } from "../port/mock";
import type { AgentPort } from "../port/port";
import type { SideProgress } from "../port/side";

function deferred() {
  let resolve!: (value: string) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<string>((ok, fail) => { resolve = ok; reject = fail; });
  return { promise, resolve, reject };
}

afterEach(() => {
  cleanup();
  window.getSelection()?.removeAllRanges();
});

function draw(extra: Partial<AgentPort> = {}) {
  const port = Object.assign(new MockPort(), extra) as unknown as AgentPort;
  const onClose = vi.fn();
  const view = render(<SidePanel port={port} onClose={onClose} />);
  return { port, onClose, view };
}

function select(text: string) {
  const host = document.createElement("p");
  host.textContent = text;
  document.body.appendChild(host);
  const range = document.createRange();
  range.selectNodeContents(host);
  const sel = window.getSelection()!;
  sel.removeAllRanges();
  sel.addRange(range);
  return host;
}

describe("the side conversation panel", () => {
  it("opens a conversation of its own and answers a question in it", async () => {
    const { port } = draw();
    const open = vi.spyOn(port, "openSide");
    const ask = vi.spyOn(port, "askSide");
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "这个函数做什么？");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(ask).toHaveBeenCalledOnce());
    expect(open.mock.calls.length).toBeLessThanOrEqual(1);
    expect(ask.mock.calls[0]![1]).toBe("这个函数做什么？");
    expect(ask.mock.calls[0]![2]).toEqual([]);
    expect(await screen.findByText(/只读回答/)).toBeTruthy();
    expect(screen.getByText("这个函数做什么？")).toBeTruthy();
  });

  it("carries an answer back to the main input as a quotation", async () => {
    const onQuote = vi.fn();
    const port = new MockPort() as unknown as AgentPort;
    render(<SidePanel port={port} onClose={vi.fn()} onQuote={onQuote} />);
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "这个函数做什么？");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await userEvent.click(await screen.findByRole("button", { name: "把这段回答引用到主输入框" }));
    expect(onQuote).toHaveBeenCalledOnce();
    expect(onQuote.mock.calls[0]![0]).toMatch(/^> .*只读回答/);
  });

  it("draws the answer as markdown but leaves the question as typed", async () => {
    const { port } = draw();
    vi.spyOn(port, "askSide").mockResolvedValue("先看 **入口函数** 再看 `main`");
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "写 **粗** 的问题");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    const answer = await waitFor(() => {
      const el = document.querySelector('.sideturn[data-role="side"]') as HTMLElement;
      expect(el.querySelector("strong")).toBeTruthy();
      return el;
    });
    expect(answer.querySelector("strong")!.textContent).toBe("入口函数");
    expect(answer.querySelector("code")!.textContent).toBe("main");
    expect(answer.textContent).not.toContain("**");
    expect(document.querySelector('.sideturn[data-role="me"]')!.textContent).toContain("**粗**");
  });

  it("brings the selected text of the main conversation along as a quote, once", async () => {
    const { port } = draw();
    const ask = vi.spyOn(port, "askSide");
    const quoted = select("第 3 行的 panic 是怎么来的");
    await userEvent.click(await screen.findByRole("button", { name: "引用选中内容" }));
    expect(within(document.querySelector(".sidequotes") as HTMLElement).getByText(/第 3 行的 panic/)).toBeTruthy();
    quoted.remove();
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "解释一下");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(ask).toHaveBeenCalledWith(expect.any(String), "解释一下", ["第 3 行的 panic 是怎么来的"], expect.any(Function)));

    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "再问一个");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(ask).toHaveBeenCalledTimes(2));
    expect(ask.mock.calls[1]![2]).toEqual([]);
  });

  it("lets a quote be taken back before it is sent", async () => {
    const { port } = draw();
    const ask = vi.spyOn(port, "askSide");
    select("先引用再取消");
    await userEvent.click(await screen.findByRole("button", { name: "引用选中内容" }));
    await userEvent.click(screen.getByRole("button", { name: /移除引用/ }));
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "问题");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(ask).toHaveBeenCalledWith(expect.any(String), "问题", [], expect.any(Function)));
  });

  it("discards the conversation when it is closed", async () => {
    const { port, onClose } = draw();
    const close = vi.spyOn(port, "closeSide");
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "问");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText(/只读回答/);
    await userEvent.click(screen.getByRole("button", { name: "关闭辅助会话" }));
    expect(close).toHaveBeenCalledOnce();
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("closes the conversation it opened even when it is unmounted without a click", async () => {
    const { port, view } = draw();
    const close = vi.spyOn(port, "closeSide");
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "问");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText(/只读回答/);
    view.unmount();
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
  });

  it("says so when the question fails, and keeps what was typed", async () => {
    const { port } = draw();
    vi.spyOn(port, "askSide").mockRejectedValue(new Error("模型没有响应"));
    const box = await screen.findByRole("textbox", { name: "向辅助会话提问" });
    await userEvent.type(box, "我的问题");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    expect((await screen.findByRole("alert")).textContent).toContain("模型没有响应");
    expect((box as HTMLTextAreaElement).value).toBe("我的问题");
  });

  it("holds the send button while an answer is being written, and for an empty question", async () => {
    const { port } = draw();
    let release!: (v: string) => void;
    vi.spyOn(port, "askSide").mockImplementation(() => new Promise((r) => { release = r; }));
    const send = screen.getByRole("button", { name: "发送" }) as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    await userEvent.type(await screen.findByRole("textbox", { name: "向辅助会话提问" }), "q");
    await userEvent.click(send);
    await waitFor(() => expect(send.disabled).toBe(true));
    release("done");
    await screen.findByText("done");
  });

  it("shows the submitted question and streamed answer before the request finishes", async () => {
    const { port } = draw();
    const pending = deferred();
    let progress!: (event: SideProgress) => void;
    vi.spyOn(port, "askSide").mockImplementation((_id, _question, _quotes, notify) => {
      progress = notify!;
      return pending.promise;
    });
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "explain the failure");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    expect(document.querySelector('.sideturn[data-role="me"]')?.textContent).toBe("explain the failure");
    act(() => progress({ kind: "thinking" }));
    expect(screen.getByRole("status").textContent).toContain("思考中");
    act(() => progress({ kind: "tool", tool: "read_file" }));
    expect(screen.getByRole("status").textContent).toContain("read_file");
    act(() => progress({ kind: "text", text: "First part" }));
    await screen.findByText("First part");
    expect((screen.getByRole("textbox", { name: "向辅助会话提问" }) as HTMLTextAreaElement).readOnly).toBe(true);
    pending.resolve("First part, complete");
    await screen.findByText("First part, complete");
    expect(screen.queryByText("First part", { exact: true })).toBeNull();
    expect(screen.getAllByText("explain the failure")).toHaveLength(1);
  });

  it("empties the input as soon as the question is sent, and gives it back only if the ask fails", async () => {
    const { port } = draw();
    const pending = deferred();
    vi.spyOn(port, "askSide").mockImplementation(() => pending.promise);
    const box = () => screen.getByRole("textbox", { name: "向辅助会话提问" }) as HTMLTextAreaElement;
    await userEvent.type(box(), "这个是什么");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    expect(box().value).toBe("");
    pending.reject(new Error("offline"));
    await waitFor(() => expect(box().value).toBe("这个是什么"));
  });

  it("renders Markdown while an answer is streaming and keeps the question literal", async () => {
    const { port } = draw();
    const pending = deferred();
    let progress!: (event: SideProgress) => void;
    vi.spyOn(port, "askSide").mockImplementation((_id, _question, _quotes, notify) => {
      progress = notify!;
      return pending.promise;
    });
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "Explain **this**");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    act(() => progress({ kind: "text", text: "**Live evidence** and `value`" }));
    await waitFor(() => expect(document.querySelector('.sideturn[data-role="side"] strong')?.textContent).toBe("Live evidence"));
    expect(document.querySelector('.sideturn[data-role="side"] code')?.textContent).toBe("value");
    expect(document.querySelector('.sideturn[data-role="me"]')?.textContent).toBe("Explain **this**");
    expect(document.querySelector('.sideturn[data-role="me"] strong')).toBeNull();
    pending.resolve("**Complete evidence** and `result`");
    await waitFor(() => expect(document.querySelector('.sideturn[data-role="side"] strong')?.textContent).toBe("Complete evidence"));
    expect(document.querySelector('.sideturn[data-role="side"] code')?.textContent).toBe("result");
    expect(screen.queryByText("Live evidence")).toBeNull();
  });

  it("keeps an interrupted partial answer distinct from a completed retry", async () => {
    const { port } = draw();
    const pending = deferred();
    let progress!: (event: SideProgress) => void;
    const ask = vi.spyOn(port, "askSide").mockImplementationOnce((_id, _question, _quotes, notify) => {
      progress = notify!;
      return pending.promise;
    }).mockResolvedValueOnce("Complete retry answer");
    const box = screen.getByRole("textbox", { name: "向辅助会话提问" }) as HTMLTextAreaElement;
    await userEvent.type(box, "keep my question");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    act(() => progress({ kind: "text", text: "Incomplete evidence" }));
    await screen.findByText("Incomplete evidence");
    pending.reject(new Error("connection lost"));
    expect((await screen.findByRole("alert")).textContent).toContain("connection lost");
    expect(screen.getByText("已中断")).toBeTruthy();
    expect(box.readOnly).toBe(false);
    expect(box.value).toBe("keep my question");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await screen.findByText("Complete retry answer");
    expect(screen.queryByText("Incomplete evidence")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(document.querySelectorAll('.sideturn[data-role="me"]')).toHaveLength(1);
    expect(ask.mock.calls[0]![0]).toBe(ask.mock.calls[1]![0]);
  });

  it("keeps streaming while collapsed and exposes that same answer when expanded", async () => {
    const { port } = draw();
    const pending = deferred();
    let progress!: (event: SideProgress) => void;
    const ask = vi.spyOn(port, "askSide").mockImplementation((_id, _question, _quotes, notify) => {
      progress = notify!;
      return pending.promise;
    });
    const close = vi.spyOn(port, "closeSide");
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "question");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await userEvent.click(screen.getByRole("button", { name: "收起辅助会话" }));
    act(() => progress({ kind: "text", text: "Still answering" }));
    await userEvent.click(screen.getByRole("button", { name: "展开辅助会话" }));
    expect(screen.getByText("Still answering")).toBeTruthy();
    expect(ask).toHaveBeenCalledOnce();
    expect(close).not.toHaveBeenCalled();
    pending.resolve("Final answer");
    await screen.findByText("Final answer");
  });

  it("does not send a question after being closed while its conversation is opening", async () => {
    const opening = deferred();
    const { port, onClose, view } = draw({ openSide: () => opening.promise });
    const ask = vi.spyOn(port, "askSide");
    const close = vi.spyOn(port, "closeSide");
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "question");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await userEvent.click(screen.getByRole("button", { name: "关闭辅助会话" }));
    expect(onClose).toHaveBeenCalledOnce();
    view.unmount();
    opening.resolve("closed-before-opened");
    await waitFor(() => expect(close).toHaveBeenCalledExactlyOnceWith("closed-before-opened"));
    expect(ask).not.toHaveBeenCalled();
  });
});


describe("collapsing a side question without discarding it", () => {
  it("keeps draft and quotes while hiding the panel controls", async () => {
    const { port } = draw();
    const close = vi.spyOn(port, "closeSide");
    const ask = vi.spyOn(port, "askSide");
    const quoted = select("keep this quote");
    await userEvent.click(screen.getByRole("button", { name: "引用选中内容" }));
    quoted.remove();
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "draft");
    const collapse = screen.getByRole("button", { name: "收起辅助会话" });
    await userEvent.click(collapse);
    expect(collapse.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(collapse);
    expect(screen.queryByRole("textbox", { name: "向辅助会话提问" })).toBeNull();
    expect(screen.queryByRole("button", { name: "发送" })).toBeNull();
    expect(screen.queryByRole("button", { name: "移除引用 1" })).toBeNull();
    expect(close).not.toHaveBeenCalled();
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "关闭辅助会话" }));
    await userEvent.click(screen.getByRole("button", { name: "展开辅助会话" }));
    expect((screen.getByRole("textbox", { name: "向辅助会话提问" }) as HTMLTextAreaElement).value).toBe("draft");
    expect(screen.getByText("keep this quote")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(ask).toHaveBeenCalledWith(expect.any(String), "draft", ["keep this quote"], expect.any(Function)));
  });

  it.each([false, true])("finishes the pending answer while collapsed (failure=%s)", async (failure) => {
    const { port } = draw();
    let finish!: (text: string) => void;
    let fail!: (reason: Error) => void;
    vi.spyOn(port, "askSide").mockImplementation(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
    const open = vi.spyOn(port, "openSide");
    const close = vi.spyOn(port, "closeSide");
    await userEvent.type(screen.getByRole("textbox", { name: "向辅助会话提问" }), "question");
    await userEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(open).toHaveBeenCalledOnce());
    await userEvent.click(screen.getByRole("button", { name: "收起辅助会话" }));
    if (failure) fail(new Error("answer failed")); else finish("answer finished");
    await waitFor(() => expect(document.querySelector(failure ? ".sideerr" : '.sideturn[data-role="side"]')?.textContent).toContain(failure ? "answer failed" : "answer finished"));
    expect(close).not.toHaveBeenCalled();
    expect(screen.queryByRole("textbox", { name: "向辅助会话提问" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "展开辅助会话" }));
    expect((screen.getByRole("textbox", { name: "向辅助会话提问" }) as HTMLTextAreaElement).value).toBe(failure ? "question" : "");
    expect(open).toHaveBeenCalledOnce();
    await userEvent.click(screen.getByRole("button", { name: "收起辅助会话" }));
    fireEvent.keyDown(screen.getByRole("button", { name: "展开辅助会话" }), { key: "Escape" });
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
  });
});
