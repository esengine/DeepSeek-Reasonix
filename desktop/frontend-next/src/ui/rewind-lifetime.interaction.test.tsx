// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { RewindResult } from "../port/port";
import { useRewindActions } from "./rewind";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const receipt = { ok: true, conversationOk: true, undoAvailable: true, transactionId: "tx-1", deleted: ["one.txt"] };
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: Error) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; }
function setup() {
  const port = new MockPort();
  vi.spyOn(port, "commitRewind").mockResolvedValue(receipt);
  vi.spyOn(port, "undoRewind").mockResolvedValue();
  vi.spyOn(port, "prepareRewind").mockResolvedValue({ planId: "plan-1", turn: 1, coverage: "full", canFiles: true, canConversation: true, fileCount: 1, requiresConfirmation: false });
  const reload = vi.fn(); const restore = vi.fn();
  const hook = renderHook(({ session, port }) => useRewindActions(port, reload, restore, session), { initialProps: { session: "session-a", port } });
  return { ...hook, port, reload, restore };
}

describe("conversation-owned restore receipt", () => {
  it("retains the receipt through rerenders, dismisses explicitly, and never replays a dismissed receipt", async () => {
    const h = setup();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    expect(h.result.current.restoreNotice?.tx).toBe("tx-1");
    h.rerender({ session: "session-a", port: h.port });
    expect(h.result.current.restoreNotice?.tx).toBe("tx-1");
    act(() => h.result.current.dismissRestore());
    h.rerender({ session: "session-a", port: h.port });
    expect(h.result.current.restoreNotice).toBeNull();
  });

  it.each(["session", "port", "unmount"])("retires a pending commit on %s change without restoring text or reloading", async (change) => {
    const h = setup(); const pending = deferred<RewindResult>();
    vi.mocked(h.port.commitRewind).mockReturnValue(pending.promise);
    let operation!: Promise<RewindResult>;
    act(() => { operation = h.result.current.onCommitRewind("plan-1", "old draft"); });
    if (change === "unmount") h.unmount();
    else h.rerender({ session: change === "session" ? "session-b" : "session-a", port: change === "port" ? new MockPort() : h.port });
    await act(async () => { pending.resolve(receipt); await operation; });
    expect(h.reload).not.toHaveBeenCalled(); expect(h.restore).not.toHaveBeenCalled();
    if (change !== "unmount") expect(h.result.current.restoreNotice).toBeNull();
  });

  it("does not resurrect a receipt or allow stale undo after leaving and returning to a session", async () => {
    const h = setup();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    const oldUndo = h.result.current.onUndoRewind;
    h.rerender({ session: "session-b", port: h.port });
    h.rerender({ session: "session-a", port: h.port });
    await act(async () => { await oldUndo("tx-1"); });
    expect(h.result.current.restoreNotice).toBeNull(); expect(h.port.undoRewind).not.toHaveBeenCalled();
  });

  it("does not advance a pending preparation into a commit after changing conversation", async () => {
    const h = setup(); const pending = deferred<Awaited<ReturnType<MockPort["prepareRewind"]>>>();
    vi.mocked(h.port.prepareRewind).mockReturnValue(pending.promise);
    const prepare = h.result.current.onPrepareRewind(1, "both");
    const rejected = expect(prepare).rejects.toThrow("会话已切换，请重新选择还原位置");
    h.rerender({ session: "session-b", port: h.port });
    await act(async () => { pending.resolve({ planId: "plan-1", turn: 1, coverage: "full", canFiles: true, canConversation: true, fileCount: 1, requiresConfirmation: false }); await rejected; });
    expect(h.port.commitRewind).not.toHaveBeenCalled();
  });

  it("coalesces repeated commits and undo clicks and reloads each successful operation once", async () => {
    const h = setup(); const commit = deferred<RewindResult>(); const undo = deferred<void>();
    vi.mocked(h.port.commitRewind).mockReturnValue(commit.promise);
    let first!: Promise<RewindResult>; let second!: Promise<RewindResult>;
    act(() => { first = h.result.current.onCommitRewind("plan-1"); second = h.result.current.onCommitRewind("plan-1"); });
    expect(first).toBe(second); expect(h.port.commitRewind).toHaveBeenCalledTimes(1);
    await act(async () => { commit.resolve(receipt); await first; });
    vi.mocked(h.port.undoRewind).mockReturnValue(undo.promise);
    let a!: Promise<void>; let b!: Promise<void>;
    act(() => { a = h.result.current.onUndoRewind("tx-1"); b = h.result.current.onUndoRewind("tx-1"); });
    expect(a).toBe(b); expect(h.port.undoRewind).toHaveBeenCalledExactlyOnceWith("tx-1");
    expect(h.result.current.restoreNotice?.working).toBe(true);
    await act(async () => { undo.resolve(); await a; });
    expect(h.result.current.restoreNotice).toBeNull(); expect(h.reload).toHaveBeenCalledTimes(2);
  });

  it("keeps a refused undo available for retry and reloads only when it succeeds", async () => {
    const h = setup();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    vi.mocked(h.port.undoRewind).mockRejectedValueOnce(new Error("Workspace changed"));
    await act(async () => { await h.result.current.onUndoRewind("tx-1"); });
    expect(h.result.current.restoreNotice).toMatchObject({ tx: "tx-1", working: false, error: "Workspace changed" });
    expect(h.reload).toHaveBeenCalledTimes(1);
    await act(async () => { await h.result.current.onUndoRewind("tx-1"); });
    expect(h.result.current.restoreNotice).toBeNull(); expect(h.reload).toHaveBeenCalledTimes(2);
  });

  it.each([{}, { undoAvailable: false, transactionId: "tx-2" }, { undoAvailable: true }])("does not offer undo without a reversible transaction: %j", async (result) => {
    const h = setup(); vi.mocked(h.port.commitRewind).mockResolvedValue({ ok: true, conversationOk: true, ...result });
    await act(async () => { await h.result.current.onCommitRewind("plan-1", "prompt"); });
    expect(h.result.current.restoreNotice).toBeNull(); expect(h.restore).toHaveBeenCalledExactlyOnceWith("prompt");
  });

  it("replaces the receipt after a later restore and rejects an earlier transaction", async () => {
    const h = setup();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    vi.mocked(h.port.commitRewind).mockResolvedValue({ ...receipt, transactionId: "tx-2" });
    await act(async () => { await h.result.current.onCommitRewind("plan-2"); await h.result.current.onUndoRewind("tx-1"); });
    expect(h.result.current.restoreNotice?.tx).toBe("tx-2"); expect(h.port.undoRewind).not.toHaveBeenCalled();
  });

  it("refuses a different plan while a commit is pending", async () => {
    const h = setup(); const pending = deferred<RewindResult>();
    vi.mocked(h.port.commitRewind).mockReturnValue(pending.promise);
    let first!: Promise<RewindResult>;
    act(() => { first = h.result.current.onCommitRewind("plan-1"); });
    await expect(h.result.current.onCommitRewind("plan-2")).rejects.toThrow("正在还原…");
    expect(h.port.commitRewind).toHaveBeenCalledExactlyOnceWith("plan-1");
    await act(async () => { pending.resolve(receipt); await first; });
  });

  it("retires the receipt when a new run starts", async () => {
    const port = new MockPort();
    vi.spyOn(port, "commitRewind").mockResolvedValue(receipt);
    const hook = renderHook(({ running }) => useRewindActions(port, vi.fn(), vi.fn(), "session-a", running), { initialProps: { running: false } });
    await act(async () => { await hook.result.current.onCommitRewind("plan-1"); });
    expect(hook.result.current.restoreNotice?.tx).toBe("tx-1");
    hook.rerender({ running: true });
    expect(hook.result.current.restoreNotice).toBeNull();
    hook.rerender({ running: false });
    expect(hook.result.current.restoreNotice).toBeNull();
  });

  it("retires an older restore receipt after a successful single-file revert", async () => {
    const h = setup();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    vi.spyOn(h.port, "commitFileRevert").mockResolvedValue({ ok: true, transactionId: "file-tx" });
    await act(async () => { await h.result.current.onCommitFileRevert("file-plan"); });
    expect(h.result.current.restoreNotice).toBeNull();
    expect(h.reload).toHaveBeenCalledTimes(1);
  });

  it("ignores late undo completion after switching sessions", async () => {
    const h = setup(); const pending = deferred<void>();
    await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
    vi.mocked(h.port.undoRewind).mockReturnValue(pending.promise);
    let operation!: Promise<void>;
    act(() => { operation = h.result.current.onUndoRewind("tx-1"); });
    h.rerender({ session: "session-b", port: h.port });
    await act(async () => { pending.resolve(); await operation; });
    expect(h.reload).toHaveBeenCalledTimes(1); expect(h.result.current.restoreNotice).toBeNull();
  });
});
