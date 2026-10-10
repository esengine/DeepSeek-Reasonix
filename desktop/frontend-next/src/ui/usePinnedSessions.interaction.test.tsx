// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import "./testkit";
import { usePinnedSessions } from "./usePinnedSessions";
import type { HubPort, TreeWorkspace } from "../port/hub";

const KEY = "reasonix:pinned-sessions";
beforeEach(() => localStorage.clear());
afterEach(cleanup);

const rows = (...s: { path: string; pinned?: boolean }[]) =>
  [{ root: "/w", name: "w", sessions: s.map((x) => ({ name: x.path, ...x })) }] as unknown as TreeWorkspace[];
const hubOf = (over: Partial<Record<"syncPins" | "pinSession", unknown>> = {}) =>
  ({ syncPins: vi.fn(async (_p: string[]) => {}), pinSession: vi.fn(async () => {}), ...over }) as unknown as HubPort & {
    syncPins: ReturnType<typeof vi.fn>;
    pinSession: ReturnType<typeof vi.fn>;
  };

describe("what is drawn as pinned", () => {
  it("is what the kernel reports, not what this window remembers", () => {
    localStorage.setItem(KEY, JSON.stringify(["/gone.jsonl"]));
    const { result } = renderHook(() => usePinnedSessions(hubOf(), rows({ path: "/k.jsonl", pinned: true }, { path: "/gone.jsonl" }), true));
    expect([...result.current[0]]).toEqual(["/k.jsonl"]);
  });

  it("follows another window unpinning, and does not pin it back", async () => {
    const hub = hubOf();
    const { result, rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: rows({ path: "/k.jsonl", pinned: true }) } });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalledTimes(1));
    rerender({ t: rows({ path: "/k.jsonl" }) });
    expect(result.current[0].has("/k.jsonl")).toBe(false);
    await new Promise((r) => setTimeout(r, 30));
    expect(hub.syncPins).toHaveBeenCalledTimes(1);
  });

  it("unpin, reload, archive does not bring the pin back", async () => {
    const hub = hubOf();
    const { result, rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: rows({ path: "/k.jsonl", pinned: true }) } });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalled());
    act(() => result.current[2]("/k.jsonl"));
    expect(result.current[0].has("/k.jsonl")).toBe(false);
    rerender({ t: rows({ path: "/k.jsonl", pinned: true }) });
    expect(result.current[0].has("/k.jsonl")).toBe(false);
    rerender({ t: rows({ path: "/k.jsonl" }) });
    expect(result.current[0].has("/k.jsonl")).toBe(false);
    expect(hub.syncPins).toHaveBeenCalledTimes(1);
  });
});

describe("toggling", () => {
  it("shows a pin at once and keeps it until the tree agrees", async () => {
    const hub = hubOf();
    const { result, rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: rows({ path: "/x.jsonl" }) } });
    act(() => result.current[1]("/x.jsonl"));
    expect(hub.pinSession).toHaveBeenCalledWith("/x.jsonl", true);
    expect(result.current[0].has("/x.jsonl")).toBe(true);
    rerender({ t: rows({ path: "/x.jsonl", pinned: true }) });
    expect(result.current[0].has("/x.jsonl")).toBe(true);
  });

  it("withdraws a pin the kernel refused and reports it", async () => {
    const hub = hubOf({ pinSession: vi.fn().mockRejectedValue(new Error("down")) });
    const fail = vi.fn();
    const { result } = renderHook(() => usePinnedSessions(hub, rows({ path: "/x.jsonl" }), true, fail));
    act(() => result.current[1]("/x.jsonl"));
    await waitFor(() => expect(fail).toHaveBeenCalledTimes(1));
    expect(result.current[0].has("/x.jsonl")).toBe(false);
  });

  it("keeps a pin the kernel refused to drop", async () => {
    const hub = hubOf({ pinSession: vi.fn().mockRejectedValue(new Error("down")) });
    const { result } = renderHook(() => usePinnedSessions(hub, rows({ path: "/k.jsonl", pinned: true }), true));
    act(() => result.current[1]("/k.jsonl"));
    await waitFor(() => expect(hub.pinSession).toHaveBeenCalled());
    await waitFor(() => expect(result.current[0].has("/k.jsonl")).toBe(true));
  });
});

describe("pins from before the kernel kept them", () => {
  it("opens the gate with nothing to send once the tree has loaded", async () => {
    const hub = hubOf();
    const { rerender } = renderHook(({ read }) => usePinnedSessions(hub, rows({ path: "/x.jsonl" }), read), { initialProps: { read: false } });
    expect(hub.syncPins).not.toHaveBeenCalled();
    rerender({ read: true });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalledWith([]));
  });

  it("sends each once, when its conversation is first listed, then forgets it", async () => {
    localStorage.setItem(KEY, JSON.stringify(["/late.jsonl", "/x.jsonl"]));
    const hub = hubOf();
    const { result, rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: rows({ path: "/x.jsonl" }) } });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalledWith(["/x.jsonl"]));
    await waitFor(() => expect(result.current[0].has("/x.jsonl")).toBe(true));
    expect(JSON.parse(localStorage.getItem(KEY) ?? "[]")).toEqual(["/late.jsonl"]);
    rerender({ t: rows({ path: "/x.jsonl", pinned: true }, { path: "/late.jsonl" }) });
    await waitFor(() => expect(hub.syncPins).toHaveBeenLastCalledWith(["/late.jsonl"]));
    await waitFor(() => expect(localStorage.getItem(KEY)).toBeNull());
  });

  it("retries after a failed send and leaves the memory in place", async () => {
    localStorage.setItem(KEY, JSON.stringify(["/x.jsonl"]));
    const hub = hubOf({ syncPins: vi.fn().mockRejectedValueOnce(new Error("down")).mockResolvedValue(undefined) });
    const { rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: rows({ path: "/x.jsonl" }) } });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalledTimes(1));
    expect(JSON.parse(localStorage.getItem(KEY) ?? "[]")).toEqual(["/x.jsonl"]);
    rerender({ t: rows({ path: "/x.jsonl" }, { path: "/y.jsonl" }) });
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(localStorage.getItem(KEY)).toBeNull());
  });
});
