// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { useQueueActions } from "./queueactions";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import type { AgentPort, Queue } from "../port/port";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const BODY = "Review the sample changes";

const snap = (ids: string[], revision: number): Queue => ({
  revision, paused: false,
  items: ids.map((id) => ({ id, intent: "followup", state: "queued", preview: id, createdAt: "" })),
  capacity: { items: ids.length, maxItems: 64, bytes: 0, maxBytes: 1 << 20 },
});

describe("withdrawing a queued line", () => {
  it("answers one click on a line once, however many clicks arrive", async () => {
    const gone = new Set<string>();
    const port = new MockPort();
    port.readQueued = vi.fn(async (id: string) => {
      if (gone.has(id)) throw new HttpError(409, "gone", { code: "inbox.not_found" });
      return BODY;
    });
    port.cancelQueued = vi.fn(async (id: string) => {
      if (gone.has(id)) throw new HttpError(409, "gone", { code: "inbox.not_found" });
      gone.add(id);
    });
    const fail = vi.fn();
    const view = renderHook(() => useQueueActions({ port: port as unknown as AgentPort, dispatch: vi.fn(), fail, moved: 0 }));
    await act(async () => {
      void view.result.current.onQueueCancel("i1");
      void view.result.current.onQueueCancel("i1");
    });
    expect(fail).not.toHaveBeenCalled();
    expect(view.result.current.restored.n).toBe(1);
  });

  it("lets a click retry a withdrawal that failed", async () => {
    const port = new MockPort();
    port.readQueued = vi.fn(async () => BODY);
    port.cancelQueued = vi.fn().mockRejectedValueOnce(new Error("disk busy")).mockResolvedValue(undefined);
    const fail = vi.fn();
    const view = renderHook(() => useQueueActions({ port: port as unknown as AgentPort, dispatch: vi.fn(), fail, moved: 0 }));
    await act(async () => view.result.current.onQueueCancel("i1"));
    expect(fail).toHaveBeenCalledTimes(1);
    await act(async () => view.result.current.onQueueCancel("i1"));
    expect(port.cancelQueued).toHaveBeenCalledTimes(2);
    expect(view.result.current.restored.n).toBe(1);
  });

  it("keeps the newest pull when an older one answers last", async () => {
    const port = new MockPort();
    const answers: Array<(q: Queue) => void> = [];
    port.queue = vi.fn(() => new Promise<Queue>((resolve) => { answers.push(resolve); }));
    const view = renderHook((p: { moved: number }) => useQueueActions({ port: port as unknown as AgentPort, dispatch: vi.fn(), fail: vi.fn(), moved: p.moved }), { initialProps: { moved: 0 } });
    view.rerender({ moved: 1 });
    await act(async () => { answers[1](snap([], 2)); });
    await act(async () => { answers[0](snap(["i1"], 1)); });
    expect(view.result.current.queue?.items).toEqual([]);
  });
});
