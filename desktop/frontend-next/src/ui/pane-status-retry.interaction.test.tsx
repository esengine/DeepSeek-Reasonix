// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { MockPort } from "../port/mock";
import type { WireEvent } from "../port/wire";
import { Pane, type PaneReport } from "./Pane";

afterEach(() => { cleanup(); vi.useRealTimers(); });

// A pane the person is not looking at. Its state of running comes from two
// reads of the kernel — the frames of its stream, and the status snapshot a turn
// boundary asks for — and when the network drops under the second one the pane
// has no other way to hear that the turn ended.
async function mount() {
  const port = new MockPort();
  const snapshot = await port.status();
  const kernel = { running: false, failing: 0, reads: 0 };
  vi.spyOn(port, "status").mockImplementation(async () => {
    kernel.reads++;
    if (kernel.failing > 0) {
      kernel.failing--;
      throw new TypeError("network down");
    }
    return { ...snapshot, running: kernel.running };
  });
  let deliver: ((event: WireEvent) => void) | undefined;
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    deliver = onEvent;
    return () => { deliver = undefined; return true; };
  });
  const onReport = vi.fn<(id: string, report: PaneReport) => void>();
  const view = render(
    <Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: "/workspace", name: "workspace", sessionPath: "/sessions/a.jsonl" }} title="a" active={false}
      visible={false} sideHost={null} side={false} onFocus={() => {}} onReport={onReport} onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}}
      needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />,
  );
  const send = (event: WireEvent | WireEvent["kind"]) => act(() => deliver?.(typeof event === "string" ? { kind: event } : event));
  const lastLive = () => onReport.mock.calls.at(-1)?.[1].live;
  return { kernel, send, onReport, lastLive, view };
}

it("hears that a turn ended even when the read that confirms it fails", async () => {
  const pane = await mount();
  pane.kernel.running = true;
  await pane.send("turn_started");
  await waitFor(() => expect(pane.lastLive()).toBe(true));
  pane.kernel.running = false;
  pane.kernel.failing = 1;
  await pane.send("turn_done");
  await waitFor(() => expect(pane.lastLive()).toBe(false), { timeout: 4000 });
});

it("keeps asking while the network stays down, and not in a tight loop", async () => {
  const pane = await mount();
  pane.kernel.running = true;
  await pane.send("turn_started");
  await waitFor(() => expect(pane.lastLive()).toBe(true));
  pane.kernel.running = false;
  pane.kernel.failing = 3;
  const before = pane.kernel.reads;
  await pane.send("turn_done");
  await waitFor(() => expect(pane.lastLive()).toBe(false), { timeout: 9000 });
  const asked = pane.kernel.reads - before;
  expect(asked).toBeGreaterThanOrEqual(4);
  expect(asked).toBeLessThan(8);
}, 15000);

it("stops asking when the pane goes away", async () => {
  const pane = await mount();
  pane.kernel.running = true;
  await pane.send("turn_started");
  await waitFor(() => expect(pane.lastLive()).toBe(true));
  pane.kernel.failing = 100;
  await pane.send("turn_done");
  pane.view.unmount();
  const at = pane.kernel.reads;
  await new Promise((resolve) => setTimeout(resolve, 2500));
  expect(pane.kernel.reads).toBe(at);
}, 10000);

it("settles buffered background reasoning before adopting an idle status", async () => {
  const pane = await mount();
  await waitFor(() => expect(pane.kernel.reads).toBeGreaterThan(0));
  pane.kernel.running = true;
  pane.kernel.failing = 1;
  await pane.send("turn_started");
  await waitFor(() => expect(pane.kernel.failing).toBe(0));
  await pane.send({ kind: "reasoning", text: "分析" });
  await pane.send({ kind: "reasoning", text: "完成" });
  pane.kernel.running = false;
  await waitFor(() => expect(pane.lastLive()).toBe(false), { timeout: 3000 });
  await new Promise((resolve) => setTimeout(resolve, 1200));
  expect(pane.view.container.querySelector(".think[data-live]")).toBeNull();
  const thoughts = pane.view.container.querySelectorAll(".think-label:not(.think-out)");
  expect(thoughts).toHaveLength(1);
  expect(thoughts[0].textContent).toContain("4 字");
});
