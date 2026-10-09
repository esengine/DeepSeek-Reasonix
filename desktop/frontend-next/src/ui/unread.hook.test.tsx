// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { AgentPort } from "../port/port";
import type { RuntimeView, TreeWorkspace } from "../port/hub";
import { useViewed } from "./unread";

const rt: RuntimeView = { id: "r1", base: "", root: "/w", name: "w", sessionPath: "/a" };
const book = (unread: boolean): TreeWorkspace[] => [{ root: "/w", name: "w", sessions: [{ path: "/a", name: "a", ...(unread ? { unread: true } : {}) }] }];

function setup(call: () => Promise<void>, reload: () => Promise<unknown>, rts: RuntimeView[] = [rt]) {
  const port = { markSessionViewed: vi.fn(call) } as unknown as AgentPort & { markSessionViewed: ReturnType<typeof vi.fn> };
  const ports = new Map([["r1", port as AgentPort]]);
  const view = renderHook((p: { tree: TreeWorkspace[]; rts?: RuntimeView[] }) => useViewed({ runtimes: p.rts ?? rts, ports, active: "r1", tree: p.tree, remote: {}, reload }), {
    initialProps: { tree: book(true) } as { tree: TreeWorkspace[]; rts?: RuntimeView[] },
  });
  return { port, view };
}

beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});
afterEach(() => vi.restoreAllMocks());

describe("useViewed", () => {
  it("keeps the optimistic clear when the reload after a successful call fails, until the tree next changes", async () => {
    const { view } = setup(() => Promise.resolve(), () => Promise.reject(new Error("offline")));
    await act(async () => view.result.current.view("r1"));
    expect(view.result.current.tree[0].sessions[0].unread).toBe(false);
    view.rerender({ tree: book(true) });
    await act(async () => {});
    expect(view.result.current.tree[0].sessions[0].unread).toBe(true);
  });

  it("restores the mark when the call itself is refused", async () => {
    const { view } = setup(() => Promise.reject(new Error("refused")), () => Promise.resolve());
    await act(async () => view.result.current.view("r1"));
    expect(view.result.current.tree[0].sessions[0].unread).toBe(true);
  });

  it("owes one more call for a turn that ends while the first is still in flight", async () => {
    let release: () => void = () => {};
    const { port, view } = setup(() => new Promise<void>((r) => (release = r)), () => Promise.resolve());
    await act(async () => view.result.current.view("r1"));
    act(() => view.result.current.turnDone("r1"));
    expect(port.markSessionViewed).toHaveBeenCalledTimes(1);
    await act(async () => release());
    await act(async () => {});
    expect(port.markSessionViewed).toHaveBeenCalledTimes(2);
  });

  it("drops an owed call when the pane has moved to another session by the time the first finishes", async () => {
    let release: () => void = () => {};
    const { port, view } = setup(() => new Promise<void>((r) => (release = r)), () => Promise.resolve());
    await act(async () => view.result.current.view("r1"));
    act(() => view.result.current.turnDone("r1"));
    view.rerender({ tree: book(true), rts: [{ ...rt, sessionPath: "/b" }] });
    await act(async () => release());
    await act(async () => {});
    expect(port.markSessionViewed).toHaveBeenCalledTimes(1);
  });

  it("does not repeat when nothing ended during the call", async () => {
    const { port, view } = setup(() => Promise.resolve(), () => Promise.resolve());
    await act(async () => view.result.current.view("r1"));
    await act(async () => {});
    expect(port.markSessionViewed).toHaveBeenCalledTimes(1);
  });
});
