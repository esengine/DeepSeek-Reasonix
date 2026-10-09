// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import "./testkit";
import { MockPort } from "../port/mock";
import type { Queue } from "../port/port";
import type { WireEvent } from "../port/wire";
import { Pane, type PaneReport } from "./Pane";

afterEach(cleanup);

const emptyQueue = (): Queue => ({
  revision: 1,
  paused: false,
  items: [],
  capacity: { items: 0, maxItems: 64, bytes: 0, maxBytes: 64 << 20 },
});

async function mount(queue: Queue = emptyQueue()) {
  const port = new MockPort();
  vi.spyOn(port, "queue").mockResolvedValue(queue);
  const snapshot = await port.status();
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...snapshot, running: true }));
  let deliver: ((event: WireEvent) => void) | undefined;
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    deliver = onEvent;
    return () => { deliver = undefined; return true; };
  });
  const onReport = vi.fn<(id: string, report: PaneReport) => void>();
  const view = render(
    <Pane
      port={port}
      rt={{ id: "r1", base: "/rt/r1", root: "/workspace", name: "workspace", sessionPath: "/sessions/active.jsonl" }}
      title="active"
      active
      visible
      sideHost={null}
      side={false}
      onFocus={() => {}}
      onReport={onReport}
      onSessionChanged={() => {}}
      pulse={0}
      findPulse={0}
      onSettings={() => {}}
      needsProject={false}
      onOpenProject={() => {}}
      onKeepHere={() => {}}
      theme="light"
      dockW={320}
      dockMax={640}
      onDockW={() => {}}
    />,
  );
  return { port, view, onReport, send: (ev: WireEvent) => act(() => deliver?.(ev)) };
}

const lastSteer = (onReport: ReturnType<typeof vi.fn>) => {
  const calls = onReport.mock.calls;
  return (calls[calls.length - 1]?.[1] as PaneReport | undefined)?.steer;
};

// The kernel delivers guidance at the next tool boundary, which can be before
// the HTTP receipt for the same line has come back to this window.
it("does not count a line the kernel delivered before its receipt arrived", async () => {
  const { port, view, onReport, send } = await mount();
  vi.spyOn(port, "steer").mockImplementation(async (text) => {
    send({ kind: "steer", text, itemId: "it1" } as WireEvent);
    await new Promise((r) => setTimeout(r, 20));
    return { itemId: "it1", disposition: "steer_accepted" };
  });
  await waitFor(() => expect(view.container.querySelector("textarea")).toBeTruthy());
  const box = view.container.querySelector('textarea[aria-label="任务输入"]') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: "换个思路", selectionStart: 4 } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => expect(port.steer).toHaveBeenCalled());
  await act(async () => new Promise((r) => setTimeout(r, 80)));
  expect(lastSteer(onReport) ?? 0).toBe(0);
});

it("counts a line that is still waiting in the kernel's queue", async () => {
  const { view, onReport } = await mount({
    ...emptyQueue(),
    items: [{ id: "it2", intent: "steer", state: "steer_accepted", preview: "换个思路", createdAt: "2026-10-02T00:00:00Z" }],
  });
  await waitFor(() => expect(view.container.querySelector(".queue")).toBeTruthy());
  await waitFor(() => expect(lastSteer(onReport)).toBe(1));
});
