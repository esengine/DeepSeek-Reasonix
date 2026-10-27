// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { HttpError } from "../port/port";
import type { HistoryPage } from "../port/session";
import { useHistoryPages, useHistorySearch } from "./useHistoryPages";

afterEach(cleanup);
const page = (snapshot = "one", cursor = "older"): HistoryPage => ({ snapshot, start: cursor ? 1 : 0, end: 2, total: 2, hasMoreBefore: !!cursor, nextCursor: cursor,
  messages: [{ role: "user", content: snapshot, msgIndex: 1 }], toolContext: [] });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; }

it("loads a bounded tail and coalesces overlapping older-page requests", async () => {
  const older = deferred<HistoryPage>();
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockResolvedValueOnce(page()).mockReturnValueOnce(older.promise) };
  const dispatch = vi.fn();
  const { result } = renderHook(() => useHistoryPages(port, dispatch));
  await act(async () => { await result.current.load(); });
  expect(port.history).not.toHaveBeenCalled();
  let one!: Promise<void>;
  act(() => { one = result.current.paging.more(); expect(result.current.paging.more()).toBe(one); });
  expect(port.historyPage).toHaveBeenCalledTimes(2);
  await act(async () => { older.resolve({ ...page("one", ""), start: 0, end: 1 }); await one; });
  expect(dispatch.mock.calls.at(-1)?.[0].kind).toBe("__history_prepend");
  expect(result.current.paging.hasMore).toBe(false);
});

it("falls back only when paging is unsupported, and remembers that capability", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockRejectedValue(new HttpError(404, "missing")) };
  const { result } = renderHook(() => useHistoryPages(port, vi.fn()));
  await act(async () => { await result.current.load(); });
  await act(async () => { await result.current.load(); });
  expect(port.historyPage).toHaveBeenCalledTimes(1);
  expect(port.history).toHaveBeenCalledTimes(2);
});

it("does not turn a failed page request into an expensive full-history request", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockRejectedValue(new HttpError(503, "unavailable")) };
  const { result } = renderHook(() => useHistoryPages(port, vi.fn()));
  await act(async () => { await expect(result.current.load()).rejects.toBeInstanceOf(HttpError); });
  expect(port.history).not.toHaveBeenCalled();
  expect(result.current.paging.failed).toBe(true);
});

it("supports ports without paging and servers explicitly refusing the optional capability", async () => {
  const history = vi.fn(async () => [{ role: "user" as const, content: "full history" }]);
  const dispatch = vi.fn();
  const legacy = renderHook(() => useHistoryPages({ history }, dispatch));
  await act(async () => { await legacy.result.current.load(); });
  expect(dispatch.mock.calls.at(-1)?.[0].items[0].text).toBe("full history");
  expect(legacy.result.current.paging.hasMore).toBe(false);
  legacy.unmount();
  const historyPage = vi.fn().mockRejectedValue(new HttpError(501, "unsupported", { code: "history.pagination_unsupported" }));
  const optional = renderHook(() => useHistoryPages({ history, historyPage }, dispatch));
  await act(async () => { await optional.result.current.load(); });
  expect(history).toHaveBeenCalledTimes(2);
  expect(optional.result.current.paging.failed).toBe(false);
});

it("discards an old binding's late page", async () => {
  const old = deferred<HistoryPage>();
  const dispatch = vi.fn();
  const first = { history: vi.fn(async () => []), historyPage: vi.fn(() => old.promise) };
  const second = { history: vi.fn(async () => []), historyPage: vi.fn(async () => page("new", "")) };
  const { result, rerender } = renderHook(({ port }) => useHistoryPages(port, dispatch), { initialProps: { port: first } });
  let pending!: Promise<void>;
  act(() => { pending = result.current.load(); });
  rerender({ port: second });
  await act(async () => { await result.current.load(); });
  await act(async () => { old.resolve(page("old")); await pending; });
  expect(dispatch).toHaveBeenCalledTimes(1);
  expect(result.current.paging.snapshot).toBe("new");
});

it("reloads a changed snapshot without appending stale rows", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockResolvedValueOnce(page())
    .mockRejectedValueOnce(new HttpError(409, "changed", { code: "history.snapshot_changed" })).mockResolvedValueOnce(page("new", "")) };
  const dispatch = vi.fn();
  const { result } = renderHook(() => useHistoryPages(port, dispatch));
  await act(async () => { await result.current.load(); });
  await act(async () => { await result.current.paging.more(); });
  expect(result.current.paging.snapshot).toBe("new");
  expect(dispatch.mock.calls.map(([event]) => event.kind)).toEqual(["__restore", "__restore"]);
});

it("searches beyond the loaded tail and stops requesting pages when the query is cleared", async () => {
  const older = deferred<HistoryPage>();
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockResolvedValueOnce({ ...page(), start: 3, end: 4, total: 4 }).mockReturnValueOnce(older.promise) };
  const dispatch = vi.fn();
  const { result, rerender } = renderHook(({ query }) => {
    const reading = useHistoryPages(port, dispatch);
    useHistorySearch(reading.paging, true, query);
    return reading;
  }, { initialProps: { query: "" } });
  await act(async () => { await result.current.load(); });
  rerender({ query: "needle" });
  expect(port.historyPage).toHaveBeenCalledTimes(2);
  rerender({ query: "" });
  await act(async () => { older.resolve({ ...page("one", "yet-older"), start: 1, end: 3, total: 4, messages: [{ role: "user", content: "needle", msgIndex: 1 }] }); });
  expect(dispatch.mock.calls.at(-1)?.[0].items[0].text).toBe("needle");
  expect(port.historyPage).toHaveBeenCalledTimes(2);
});

it("refuses malformed successful responses without falling back to full history", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockResolvedValue(null) };
  const { result } = renderHook(() => useHistoryPages(port, vi.fn()));
  await act(async () => { await expect(result.current.load()).rejects.toThrow("invalid history page"); });
  expect(port.history).not.toHaveBeenCalled();
});

it("continues searching when fast page responses batch busy state transitions", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn()
    .mockResolvedValueOnce({ ...page("one", "middle"), start: 4, end: 6, total: 6 })
    .mockResolvedValueOnce({ ...page("one", "first"), start: 2, end: 4, total: 6 })
    .mockResolvedValueOnce({ ...page("one", ""), start: 0, end: 2, total: 6 }) };
  const dispatch = vi.fn();
  const { result } = renderHook(() => {
    const reading = useHistoryPages(port, dispatch);
    useHistorySearch(reading.paging, true, "needle");
    return reading;
  });
  await act(async () => { await result.current.load(); });
  await waitFor(() => expect(result.current.paging.hasMore).toBe(false));
  expect(port.historyPage.mock.calls).toEqual([[], ["middle"], ["first"]]);
  expect(dispatch.mock.calls.map(([event]) => event.kind)).toEqual(["__restore", "__history_prepend", "__history_prepend"]);
});

it("searches the next page when a committed revision is the only changed read state", () => {
  const more = vi.fn(async () => {});
  const paging = { snapshot: "one", revision: 1, hasMore: true, busy: false, failed: false, earlier: 0, more, reload: vi.fn(async () => {}) };
  const { rerender } = renderHook(({ revision }) => useHistorySearch({ ...paging, revision }, true, "needle"), { initialProps: { revision: 1 } });
  expect(more).toHaveBeenCalledTimes(1);
  rerender({ revision: 2 });
  expect(more).toHaveBeenCalledTimes(2);
});

it("does not treat a missing runtime as an unsupported paging route", async () => {
  const port = { history: vi.fn(async () => []), historyPage: vi.fn().mockRejectedValue(new HttpError(404, "gone", { code: "runtime.not_found" })) };
  const { result } = renderHook(() => useHistoryPages(port, vi.fn()));
  await act(async () => { await expect(result.current.load()).rejects.toBeInstanceOf(HttpError); });
  expect(port.history).not.toHaveBeenCalled();
});
