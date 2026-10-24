// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useSidebarOpen, type SidebarOpenRequest } from "./sidebaropen";

/** The sidebar's two ways of landing: a conversation already on screen returns
 *  its pane to the reading view, and one that is not opens a new pane. */
function harness(runtimes: { id: string; sessionPath?: string }[]) {
  const openPane = vi.fn(async (_req: SidebarOpenRequest) => undefined);
  const setActive = vi.fn();
  const requests: Record<string, { to: string; n: number }>[] = [];
  const setViewRequests = vi.fn((next: unknown) => {
    // The hook hands back a function; run it against an empty map to see what
    // it would have written.
    if (typeof next === "function") {
      requests.push((next as (prev: Record<string, { to: string; n: number }>) => Record<string, { to: string; n: number }>)({}));
    }
  });
  const { result } = renderHook(() => useSidebarOpen(runtimes, openPane, setActive, setViewRequests as never, "flow"));
  return { open: result.current, openPane, setActive, requests, setViewRequests };
}

describe("picking a conversation from the sidebar", () => {
  it("returns an already-open pane to the reading view instead of opening another", async () => {
    const h = harness([{ id: "p1", sessionPath: "/s/a" }]);
    await act(async () => {
      await h.open({ sessionPath: "/s/a" });
    });
    expect(h.openPane).not.toHaveBeenCalled();
    expect(h.setActive).toHaveBeenCalledWith("p1");
    expect(h.requests.at(-1)).toEqual({ p1: { to: "flow", n: 1 } });
  });

  it("counts up, so asking twice lands twice", async () => {
    const h = harness([{ id: "p1", sessionPath: "/s/a" }]);
    await act(async () => {
      await h.open({ sessionPath: "/s/a" });
      await h.open({ sessionPath: "/s/a" });
    });
    const written = h.setViewRequests.mock.calls.map((c) => c[0]);
    expect(written).toHaveLength(2);
  });

  it("opens a pane for a conversation that is not on screen", async () => {
    const h = harness([{ id: "p1", sessionPath: "/s/a" }]);
    await act(async () => {
      await h.open({ sessionPath: "/s/b" });
    });
    expect(h.openPane).toHaveBeenCalledWith({ sessionPath: "/s/b" });
    expect(h.setActive).not.toHaveBeenCalled();
  });

  it("opens a pane when the request names a folder rather than a conversation", async () => {
    const h = harness([{ id: "p1", sessionPath: "/s/a" }]);
    await act(async () => {
      await h.open({ root: "/work" });
    });
    expect(h.openPane).toHaveBeenCalledWith({ root: "/work" });
  });
});
