// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { MockPort } from "../port/mock";
import type { WireEvent } from "../port/wire";
import { Pane, type PaneReport } from "./Pane";

afterEach(cleanup);

async function mount(initiallyRunning: boolean, visible: boolean) {
  const port = new MockPort();
  const snapshot = await port.status();
  let kernelRunning = initiallyRunning;
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...snapshot, running: kernelRunning }));
  let deliver: ((event: WireEvent) => void) | undefined;
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    deliver = onEvent;
    return () => { deliver = undefined; return true; };
  });
  const onReport = vi.fn<(id: string, report: PaneReport) => void>();
  const view = (shown: boolean) => (
    <Pane
      port={port}
      rt={{ id: "r1", base: "/rt/r1", root: "/workspace", name: "workspace", sessionPath: "/sessions/active.jsonl" }}
      title="active"
      active
      visible={shown}
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
    />
  );
  const { rerender } = render(view(visible));
  const send = (kind: WireEvent["kind"]) => act(() => deliver?.({ kind }));
  return { onReport, send, hide: () => rerender(view(false)), setRunning: (value: boolean) => { kernelRunning = value; } };
}

const reported = (onReport: ReturnType<typeof vi.fn>, live: boolean, run?: string) =>
  waitFor(() => expect(onReport).toHaveBeenCalledWith("r1", expect.objectContaining({ live, ...(run ? { run } : {}) })));

it("clears a background pane that joined after turn_started", async () => {
  const pane = await mount(true, false);
  await reported(pane.onReport, true, "running");
  pane.onReport.mockClear();
  pane.setRunning(false);
  pane.send("turn_done");
  await reported(pane.onReport, false, "idle");
});

it("clears a pane that saw turn_started before it was hidden", async () => {
  const pane = await mount(false, true);
  pane.setRunning(true);
  pane.send("turn_started");
  await reported(pane.onReport, true, "running");
  await waitFor(() => expect(pane.onReport.mock.calls.some(([, report]) => report.status?.running)).toBe(true));
  pane.hide();
  pane.onReport.mockClear();
  pane.setRunning(false);
  pane.send("turn_done");
  await reported(pane.onReport, false, "idle");
});
