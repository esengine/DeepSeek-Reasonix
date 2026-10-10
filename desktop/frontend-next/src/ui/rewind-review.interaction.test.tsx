// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { RewindResult } from "../port/port";
import { useRewindActions } from "./rewind";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const receipt = { ok: true, conversationOk: true, undoAvailable: true, transactionId: "tx-1", deleted: ["one.txt"] };
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; }
function setup() {
  const port = new MockPort();
  vi.spyOn(port, "commitRewind").mockResolvedValue(receipt);
  vi.spyOn(port, "undoRewind").mockResolvedValue();
  const reload = vi.fn(async () => {});
  const restore = vi.fn();
  const hook = renderHook(({ running, session }) => useRewindActions(port, reload, restore, session, running), { initialProps: { running: false, session: "a" } });
  return { ...hook, port, reload, restore };
}

it.each(["commit", "undo"])("synchronizes a completed %s after a same-session run overtakes its response", async (operation) => {
  const h = setup();
  if (operation === "undo") await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
  h.reload.mockClear();
  const response = deferred<RewindResult>();
  vi.mocked(h.port.commitRewind).mockReturnValue(response.promise);
  vi.mocked(h.port.undoRewind).mockImplementation(() => response.promise.then(() => {}));
  let pending!: Promise<unknown>;
  act(() => { pending = operation === "commit" ? h.result.current.onCommitRewind("plan-1", "old prompt") : h.result.current.onUndoRewind("tx-1"); });
  h.rerender({ running: true, session: "a" });
  h.rerender({ running: false, session: "a" });
  await act(async () => { response.resolve(receipt); await pending; });
  expect(h.reload).toHaveBeenCalledTimes(1);
  expect(h.result.current.restoreNotice).toBeNull();
  expect(h.restore).not.toHaveBeenCalled();
});

it("keeps the still-valid receipt when Keep current resolves a file conflict without a transaction", async () => {
  const h = setup();
  await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
  vi.spyOn(h.port, "commitFileRevert").mockResolvedValue({ ok: true, undoAvailable: false });
  await act(async () => { await h.result.current.onCommitFileRevert("file-plan", "keep_current"); });
  expect(h.result.current.restoreNotice?.tx).toBe("tx-1");
  await act(async () => { await h.result.current.onUndoRewind("tx-1"); });
  expect(h.port.undoRewind).toHaveBeenCalledExactlyOnceWith("tx-1");
});

it("waits for post-undo history synchronization instead of declaring completion early", async () => {
  const h = setup();
  await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
  const history = deferred<void>();
  h.reload.mockReturnValue(history.promise);
  let settled = false;
  let undo!: Promise<void>;
  await act(async () => { undo = h.result.current.onUndoRewind("tx-1"); void undo.then(() => { settled = true; }); });
  expect(settled).toBe(false);
  await act(async () => { history.resolve(); await undo; });
  expect(settled).toBe(true);
});

it("reports a post-undo history failure without reviving the already-consumed transaction", async () => {
  const h = setup();
  await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
  const failed = Promise.reject(new Error("History unavailable"));
  void failed.catch(() => {});
  h.reload.mockReturnValue(failed);
  await act(async () => { await h.result.current.onUndoRewind("tx-1"); });
  expect(h.result.current).toMatchObject({ reloadFailure: { error: "History unavailable", working: false }, restoreNotice: null });
  expect(h.port.undoRewind).toHaveBeenCalledExactlyOnceWith("tx-1");
});

it("coalesces refresh-only retries and ignores late failures after a session switch", async () => {
  const h = setup();
  await act(async () => { await h.result.current.onCommitRewind("plan-1"); });
  h.reload.mockRejectedValueOnce(new Error("History unavailable"));
  await act(async () => { await h.result.current.onUndoRewind("tx-1"); });
  const read = deferred<void>();
  h.reload.mockReturnValue(read.promise);
  let retry!: Promise<void>;
  act(() => {
    retry = h.result.current.onReloadSession();
    expect(h.result.current.onReloadSession()).toBe(retry);
  });
  expect(h.result.current.reloadFailure?.working).toBe(true);
  await act(async () => { read.resolve(); await retry; });
  expect(h.result.current.reloadFailure).toBeNull();
  expect(h.port.undoRewind).toHaveBeenCalledExactlyOnceWith("tx-1");
  expect(h.reload).toHaveBeenCalledTimes(3);
  let reject!: (error: Error) => void;
  h.reload.mockReturnValue(new Promise((_, fail) => { reject = fail; }));
  act(() => { retry = h.result.current.onReloadSession(); });
  h.rerender({ session: "b", running: false });
  await act(async () => { reject(new Error("Old session")); await retry; });
  expect(h.result.current.reloadFailure).toBeNull();
});

it("coalesces the same commit while its history read is pending", async () => {
  const h = setup(); const history = deferred<void>();
  h.reload.mockReturnValue(history.promise);
  let commit!: Promise<RewindResult>;
  await act(async () => { commit = h.result.current.onCommitRewind("plan-1"); });
  expect(h.result.current.restoreNotice?.working).toBe(true);
  expect(h.result.current.onCommitRewind("plan-1")).toBe(commit);
  await act(async () => { history.resolve(); await commit; });
  expect(h.port.commitRewind).toHaveBeenCalledTimes(1);
});
