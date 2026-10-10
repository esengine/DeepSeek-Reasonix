// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockPort } from "../port/mock";
import type { Decision } from "../port/session";
import type { WireEvent } from "../port/wire";
import { Pane, type PaneReport } from "./Pane";

afterEach(cleanup);

async function mount(initiallyRunning: boolean, visible: boolean, decisions: Decision[] = []) {
  const port = new MockPort();
  const snapshot = await port.status();
  let kernelRunning = initiallyRunning;
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...snapshot, decisions, running: kernelRunning }));
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
  const { rerender, container } = render(view(visible));
  const send = (kind: WireEvent["kind"]) => act(() => deliver?.({ kind }));
  const sendEvent = (event: WireEvent) => act(() => deliver?.(event));
  return { port, container, onReport, send, sendEvent, hide: () => rerender(view(false)), setRunning: (value: boolean) => { kernelRunning = value; } };
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

for (const route of ["local", "receipt"] as const) {
  it.each([false, true])(`restores the current turn label after a ${route} question answer (running=%s)`, async (running) => {
    const pane = await mount(false, true);
    await reported(pane.onReport, false, "idle");
    pane.setRunning(running);
    if (running) pane.send("turn_started");
    const answer = vi.spyOn(pane.port, "answer").mockResolvedValue(undefined);
    pane.sendEvent({ kind: "ask_request", ask: {
      id: "greeting", questions: [{ id: "name", header: "Your name", prompt: "Your name", multi: false, options: [{ label: "Native SDK" }] }],
    } });
    await screen.findByText("等待你回答");
    if (route === "local") {
      await userEvent.click(screen.getByRole("button", { name: "Native SDK" }));
      await userEvent.click(screen.getByRole("button", { name: "确认" }));
      expect(answer).toHaveBeenCalledWith("greeting", [{ questionId: "name", selected: ["Native SDK"] }]);
    } else {
      pane.sendEvent({ kind: "notice", decisionReceipt: { id: "greeting", kind: "ask", subject: "Your name: Native SDK", outcome: "answered" } });
      expect(answer).not.toHaveBeenCalled();
    }
    await waitFor(() => expect(pane.container.querySelector('[data-k="ask"]')?.getAttribute("data-prompt")).toBe("settled"));
    await screen.findByText(running ? "运行中" : "空闲");
    expect(screen.queryByText("等待你回答")).toBeNull();
  });
}
it("marks live work above the composer and removes its emphasis when done", async () => {
  const pane = await mount(false, true);
  await reported(pane.onReport, false, "idle");
  expect(document.querySelector(".studio-runstate[data-idle]")).not.toBeNull();
  pane.setRunning(true);
  pane.send("turn_started");
  await waitFor(() => expect(document.querySelector(".studio-runstate[data-running] .studio-runlabel")?.textContent).toContain("运行中"));
  expect(document.querySelector(".studio-runstate[data-idle]")).toBeNull();
  pane.setRunning(false);
  pane.send("turn_done");
  await waitFor(() => expect(document.querySelector(".studio-runstate[data-running]")).toBeNull());
  expect(document.querySelector(".studio-runstate[data-idle]")).not.toBeNull();
});

it("keeps waiting on a person distinct from active progress", async () => {
  const pane = await mount(true, true, [{ id: "approval-1", kind: "tool_approval" }]);
  await reported(pane.onReport, true, "halt");
  expect(document.querySelector(".studio-runstate[data-waiting] .studio-runlabel")).not.toBeNull();
  expect(document.querySelector(".studio-runstate[data-running]")).toBeNull();
  expect(document.querySelector(".studio-runstate[data-idle]")).toBeNull();
});
