// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import { HttpError, type HistoryMessage, type RewindResult } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { useRewindActions } from "./rewind";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const BODY = "@.reasonix/attachments/clipboard-1.png Review this image\n  Keep the indentation";
const HISTORY: HistoryMessage[] = [
  { role: "user", content: BODY, msgIndex: 0 },
  { role: "assistant", content: "The image has been reviewed.", msgIndex: 1 },
];
const rt = { id: "p1", root: "/sample", name: "Sample" } as RuntimeView;
const props = {
  rt, title: "Sample", active: true, visible: true, sideHost: null, side: false,
  onFocus() {}, onReport() {}, onSessionChanged() {}, pulse: 0, findPulse: 0,
  onSettings() {}, needsProject: false, onOpenProject() {}, onKeepHere() {},
  theme: "dark", dockW: 560, dockMax: 880, onDockW() {},
};
const box = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;
const type = (text: string) => fireEvent.change(box(), { target: { value: text, selectionStart: text.length } });

async function open(result: RewindResult = { ok: true, conversationOk: true }, original = HISTORY) {
  const port = new MockPort();
  let history = original;
  const reads = vi.spyOn(port, "history").mockImplementation(async () => history);
  vi.spyOn(port, "checkpoints").mockImplementation(async () => history.filter((m) => m.role === "user").map((m, i) => ({
    turn: i + 1, prompt: "Review this image", files: 2, msgIndex: m.msgIndex,
  })));
  vi.spyOn(port, "prepareRewind").mockImplementation(async (turn) => ({
    planId: `plan-${turn}`, turn, coverage: "full", canFiles: true, canConversation: true,
    fileCount: 2, requiresConfirmation: false,
  }));
  vi.spyOn(port, "commitRewind").mockImplementation(async (planId) => {
    if (result.conversationOk) history = history.slice(0, (Number(planId.slice(5)) - 1) * 2);
    return result;
  });
  vi.spyOn(port, "undoRewind").mockResolvedValue();
  vi.spyOn(port, "submit").mockResolvedValue({ itemId: "sent-1", disposition: "queued_followup" });
  render(<Pane {...props} port={port} />);
  await screen.findAllByRole("button", { name: "回到这里" });
  return { port, reads };
}

async function rewind(label = "只回退对话") {
  fireEvent.click(screen.getAllByRole("button", { name: "回到这里" }).at(-1)!);
  fireEvent.click(await screen.findByRole("menuitem", { name: new RegExp(label) }));
}

describe("restoring a rewound prompt to the composer", () => {
  it.each(["", BODY, "An existing draft  "])("restores the complete original beside %j", async (draft) => {
    const { port, reads } = await open();
    type(draft);
    const before = reads.mock.calls.length;
    await rewind();
    await waitFor(() => expect(reads.mock.calls.length).toBeGreaterThan(before));
    await waitFor(() => expect(screen.queryByRole("button", { name: "回到这里" })).toBeNull());
    expect(port.prepareRewind).toHaveBeenCalledWith(1, "conversation");
    expect(port.commitRewind).toHaveBeenCalledExactlyOnceWith("plan-1");
    await waitFor(() => expect(box().value).toBe(draft && draft !== BODY ? `An existing draft\n${BODY}` : BODY));
    await waitFor(() => expect(document.activeElement).toBe(box()));
  });

  it("also restores when code and conversation are rewound together", async () => {
    const { port } = await open();
    await rewind("代码和对话");
    await waitFor(() => expect(box().value).toBe(BODY));
    expect(port.prepareRewind).toHaveBeenCalledWith(1, "both");
  });

  it("uses the reported conversation outcome instead of aggregate ok", async () => {
    await open({ ok: false, conversationOk: true });
    await rewind("代码和对话");
    await waitFor(() => expect(box().value).toBe(BODY));
  });

  it.each([false, undefined])("keeps a draft when conversationOk is %j", async (conversationOk) => {
    const { reads } = await open({ ok: true, conversationOk });
    type("Keep my draft");
    const before = reads.mock.calls.length;
    await rewind();
    await waitFor(() => expect(reads.mock.calls.length).toBeGreaterThan(before));
    expect(box().value).toBe("Keep my draft");
    expect(screen.getByRole("button", { name: "回到这里" })).toBeTruthy();
  });

  it("keeps the draft through a code-only rewind", async () => {
    const { port, reads } = await open({ ok: true, conversationOk: false });
    type("Keep my draft");
    const before = reads.mock.calls.length;
    await rewind("只还原代码");
    await waitFor(() => expect(reads.mock.calls.length).toBeGreaterThan(before));
    expect(port.prepareRewind).toHaveBeenCalledWith(1, "code");
    expect(box().value).toBe("Keep my draft");
  });

  it("does not restore or reload after a rejected commit", async () => {
    const { port, reads } = await open();
    vi.mocked(port.commitRewind).mockRejectedValue(new Error("checkpoint is gone"));
    type("Keep my draft");
    const before = reads.mock.calls.length;
    await rewind();
    await screen.findByText("checkpoint is gone");
    expect(box().value).toBe("Keep my draft");
    expect(reads.mock.calls).toHaveLength(before);
  });

  it("keeps the draft when preparation fails", async () => {
    const { port } = await open();
    vi.mocked(port.prepareRewind).mockRejectedValue(new Error("cannot prepare"));
    type("Keep my draft");
    await rewind();
    await screen.findByText("cannot prepare");
    expect(port.commitRewind).not.toHaveBeenCalled();
    expect(box().value).toBe("Keep my draft");
  });

  it.each([false, true])("restores only after a confirmed commit: %j", async (confirm) => {
    const { port } = await open();
    vi.mocked(port.prepareRewind).mockResolvedValue({
      planId: "plan-1", turn: 1, coverage: "partial", canFiles: true, canConversation: true,
      fileCount: 2, requiresConfirmation: true,
    });
    type("Keep my draft");
    await rewind("代码和对话");
    await screen.findByRole("menuitem", { name: /仍还原其余部分/ });
    expect(box().value).toBe("Keep my draft");
    expect(port.commitRewind).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("menuitem", { name: confirm ? /仍还原其余部分/ : "取消" }));
    if (confirm) await waitFor(() => expect(box().value).toBe(`Keep my draft\n${BODY}`));
    else {
      expect(port.commitRewind).not.toHaveBeenCalled();
      expect(box().value).toBe("Keep my draft");
    }
  });

  it("keeps text entered while the commit is pending", async () => {
    const { port } = await open();
    let resolve!: (result: RewindResult) => void;
    vi.mocked(port.commitRewind).mockReturnValue(new Promise((done) => { resolve = done; }));
    await rewind();
    await waitFor(() => expect(port.commitRewind).toHaveBeenCalled());
    expect(box().value).toBe("");
    type("Written while waiting");
    await act(async () => resolve({ ok: true, conversationOk: true }));
    await waitFor(() => expect(box().value).toBe(`Written while waiting\n${BODY}`));
  });

  it("can restore the same prompt again without replaying an old request", async () => {
    const { port } = await open({ ok: true, conversationOk: true }, [
      ...HISTORY, ...HISTORY.map((m) => ({ ...m, msgIndex: m.msgIndex! + 2 })),
    ]);
    await rewind();
    await waitFor(() => expect(box().value).toBe(BODY));
    await waitFor(() => expect(screen.getAllByRole("button", { name: "回到这里" })).toHaveLength(1));
    type("");
    await rewind();
    await waitFor(() => expect(box().value).toBe(BODY));
    expect(port.commitRewind).toHaveBeenCalledTimes(2);
  });

  it("lets the restored attachment reference be edited and submitted", async () => {
    const { port } = await open();
    await rewind();
    await waitFor(() => expect(box().value).toBe(BODY));
    const edited = BODY.replace("Review this image", "Explain this image");
    type(edited);
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(port.submit).toHaveBeenCalledWith(edited, undefined));
    expect(box().value).toBe("");
  });

  it("keeps the composer draft when a card is edited and resent", async () => {
    const { port } = await open();
    type("Separate draft");
    fireEvent.click(screen.getByRole("button", { name: "改写" }));
    const edit = screen.getByRole("textbox", { name: "改写这条消息" });
    fireEvent.change(edit, { target: { value: "A revised prompt" } });
    fireEvent.click(screen.getByRole("button", { name: "改完重发" }));
    await waitFor(() => expect(port.submit).toHaveBeenCalledWith("A revised prompt", undefined));
    expect(box().value).toBe("Separate draft");
  });

  it.each(["prepareRewind", "commitRewind"] as const)("keeps a failed edit available for retry after %s rejects", async (stage) => {
    const { port, reads } = await open();
    vi.mocked(port[stage]).mockRejectedValueOnce(new HttpError(500, "checkpoint is gone"));
    type("Separate draft");
    fireEvent.click(screen.getByRole("button", { name: "改写" }));
    const edit = screen.getByRole("textbox", { name: "改写这条消息" }) as HTMLTextAreaElement;
    const revised = "A revised prompt\n  Keep this indentation  ";
    fireEvent.change(edit, { target: { value: revised } });
    const before = reads.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "改完重发" }));
    await screen.findByText("checkpoint is gone");
    expect(screen.getByRole("textbox", { name: "改写这条消息" })).toBe(edit);
    expect(edit.value).toBe(revised);
    expect(edit.closest(".reask")?.textContent).toContain("checkpoint is gone");
    expect(box().value).toBe("Separate draft");
    expect(port.submit).not.toHaveBeenCalled();
    expect(reads.mock.calls).toHaveLength(before);

    fireEvent.click(screen.getByRole("button", { name: "改完重发" }));
    await waitFor(() => expect(port.submit).toHaveBeenCalledExactlyOnceWith(revised.trim(), undefined));
    expect(screen.queryByRole("textbox", { name: "改写这条消息" })).toBeNull();
    expect(box().value).toBe("Separate draft");
  });

  it("keeps the edited words when the prepared plan cannot rewind the conversation", async () => {
    const { port } = await open();
    vi.mocked(port.prepareRewind).mockResolvedValue({
      planId: "unavailable", turn: 1, coverage: "full", canFiles: false, canConversation: false,
      fileCount: 0, requiresConfirmation: false, disabledReason: "conversation boundary is unavailable",
    });
    fireEvent.click(screen.getByRole("button", { name: "改写" }));
    const edit = screen.getByRole("textbox", { name: "改写这条消息" }) as HTMLTextAreaElement;
    fireEvent.change(edit, { target: { value: "Keep these revised words" } });
    fireEvent.click(screen.getByRole("button", { name: "改完重发" }));
    await screen.findByText("conversation boundary is unavailable");
    expect(screen.getByRole("textbox", { name: "改写这条消息" })).toBe(edit);
    expect(edit.value).toBe("Keep these revised words");
    expect(port.commitRewind).not.toHaveBeenCalled();
    expect(port.submit).not.toHaveBeenCalled();
  });

  it.each(["prepareRewind", "commitRewind"] as const)("reports a regeneration failure when %s rejects", async (stage) => {
    const { port, reads } = await open();
    vi.mocked(port[stage]).mockRejectedValueOnce(new HttpError(500, "checkpoint is gone"));
    const before = reads.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "重新生成" }));
    fireEvent.click(screen.getByRole("menuitem", { name: /按当前配置重试/ }));
    await screen.findByText("checkpoint is gone");
    expect(screen.getByText("The image has been reviewed.")).toBeTruthy();
    expect(port.submit).not.toHaveBeenCalled();
    expect(reads.mock.calls).toHaveLength(before);
  });

  it("leaves restoration out of undo, single-file revert and commits without a prompt", async () => {
    const port = new MockPort();
    vi.spyOn(port, "commitRewind").mockResolvedValue({ ok: true, conversationOk: true });
    const undo = vi.spyOn(port, "undoRewind").mockResolvedValue();
    const revert = vi.spyOn(port, "commitFileRevert").mockResolvedValue({ ok: true });
    const reload = vi.fn();
    const restore = vi.fn();
    const { result } = renderHook(() => useRewindActions(port, reload, restore));
    await act(async () => { await result.current.onCommitRewind("plan-1"); });
    expect(reload).toHaveBeenCalledTimes(1);
    await act(async () => { await result.current.onUndoRewind("tx-1"); });
    expect(undo).toHaveBeenCalledExactlyOnceWith("tx-1");
    expect(reload).toHaveBeenCalledTimes(2);
    await act(async () => { await result.current.onCommitFileRevert("file-plan", "overwrite"); });
    expect(revert).toHaveBeenCalledExactlyOnceWith("file-plan", "overwrite");
    expect(reload).toHaveBeenCalledTimes(2);
    expect(restore).not.toHaveBeenCalled();
  });
});
