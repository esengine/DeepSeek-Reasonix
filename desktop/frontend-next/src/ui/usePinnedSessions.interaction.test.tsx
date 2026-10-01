// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
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
