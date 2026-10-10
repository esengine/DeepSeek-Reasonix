// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import "./testkit";
import { usePinnedSessions } from "./usePinnedSessions";
import type { HubPort, TreeWorkspace } from "../port/hub";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

const tree = [{ root: "/w", name: "w", sessions: [{ path: "/k.jsonl", name: "k", pinned: true }, { path: "/x.jsonl", name: "x" }] }] as unknown as TreeWorkspace[];

describe("pin sync", () => {
  it("sends the union of the kernel's pins and the window's, and adopts it", async () => {
    localStorage.setItem("reasonix:pinned-sessions", JSON.stringify(["/x.jsonl"]));
    const hub = { syncPins: vi.fn(async () => {}), pinSession: vi.fn(async () => {}) } as unknown as HubPort;
    const { result } = renderHook(() => usePinnedSessions(hub, tree, true));
    await waitFor(() => expect(hub.syncPins).toHaveBeenCalled());
    expect([...(hub.syncPins as ReturnType<typeof vi.fn>).mock.calls[0][0]].sort()).toEqual(["/k.jsonl", "/x.jsonl"]);
    await waitFor(() => expect(result.current[0].has("/k.jsonl")).toBe(true));
  });

  it("does not send before the tree has loaded, and retries after a failure", async () => {
    const send = vi.fn().mockRejectedValueOnce(new Error("down")).mockResolvedValue(undefined);
    const hub = { syncPins: send, pinSession: vi.fn(async () => {}) } as unknown as HubPort;
    const { rerender } = renderHook(({ read, t }) => usePinnedSessions(hub, t, read), { initialProps: { read: false, t: tree } });
    expect(send).not.toHaveBeenCalled();
    rerender({ read: true, t: tree });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    rerender({ read: true, t: [...tree] });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    rerender({ read: true, t: [...tree] });
    await new Promise((r) => setTimeout(r, 30));
    expect(send).toHaveBeenCalledTimes(2);
  });
});

describe("pin persistence", () => {
  it("rolls a pin back and reports it when the kernel refuses", async () => {
    const hub = { syncPins: vi.fn(async () => {}), pinSession: vi.fn().mockRejectedValue(new Error("down")) } as unknown as HubPort;
    const fail = vi.fn();
    const { result } = renderHook(() => usePinnedSessions(hub, [], false, fail));
    act(() => result.current[1]("/x.jsonl"));
    await waitFor(() => expect(fail).toHaveBeenCalledTimes(1));
    expect(result.current[0].has("/x.jsonl")).toBe(false);
    expect(JSON.parse(localStorage.getItem("reasonix:pinned-sessions") ?? "[]")).toEqual([]);
  });

  it("restores a pin when the kernel refuses to drop it", async () => {
    localStorage.setItem("reasonix:pinned-sessions", JSON.stringify(["/k.jsonl"]));
    const hub = { syncPins: vi.fn(async () => {}), pinSession: vi.fn().mockRejectedValue(new Error("down")) } as unknown as HubPort;
    const { result } = renderHook(() => usePinnedSessions(hub, [], false));
    act(() => result.current[1]("/k.jsonl"));
    await waitFor(() => expect(result.current[0].has("/k.jsonl")).toBe(true));
  });

  it("tells the kernel about a legacy pin in a workspace listed after the first sync", async () => {
    localStorage.setItem("reasonix:pinned-sessions", JSON.stringify(["/late.jsonl"]));
    const send = vi.fn(async (_paths: string[]) => {});
    const hub = { syncPins: send, pinSession: vi.fn(async () => {}) } as unknown as HubPort;
    const { rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: tree } });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    const later = [...tree, { root: "/w2", name: "w2", sessions: [{ path: "/late.jsonl", name: "late" }] }] as unknown as TreeWorkspace[];
    rerender({ t: later });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1][0]).toContain("/late.jsonl");
  });

  it("does not resend while every remembered pin is already pinned in the kernel", async () => {
    const send = vi.fn(async () => {});
    const hub = { syncPins: send, pinSession: vi.fn(async () => {}) } as unknown as HubPort;
    const { rerender } = renderHook(({ t }) => usePinnedSessions(hub, t, true), { initialProps: { t: tree } });
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    rerender({ t: [...tree] });
    await new Promise((r) => setTimeout(r, 30));
    expect(send).toHaveBeenCalledTimes(1);
  });
});
