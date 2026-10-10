// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { WireEvent } from "../port/wire";
import { Pane } from "./Pane";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

async function mount(running = false, side = false) {
  const port = new MockPort();
  const snapshot = await port.status();
  let kernelRunning = running;
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...snapshot, running: kernelRunning }));
  const subscribers = new Set<(event: WireEvent) => void>();
  let gap: (() => void) | undefined;
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap) => {
    subscribers.add(onEvent);
    gap = onGap;
    return () => subscribers.delete(onEvent);
  });
  const submit = vi.spyOn(port, "submit").mockResolvedValue();
  const queue = vi.spyOn(port, "queueFollowup");
  const context = vi.spyOn(port, "context");
  const settings = vi.fn();
  const host = document.createElement("aside");
  const view = render(<>
    <Pane port={port} rt={{ id: "compact", root: "/workspace", name: "Workspace", base: "/rt/compact" }}
      title="Workspace" active visible sideHost={side ? host : null} side={side}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={settings} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="light" dockW={320} dockMax={640} onDockW={() => {}} />
  </>);
  view.container.append(host);
  await screen.findByRole("button", { name: "查看上下文与压缩" });
  fireEvent.click(screen.getByRole("button", { name: "查看上下文与压缩" }));
  await waitFor(() => expect(view.container.querySelector('.studio-context-anchor[data-open]')).toBeTruthy());
  const send = (event: WireEvent) => act(() => { subscribers.forEach((fn) => fn(event)); });
  const card = () => within(screen.getByRole("dialog", { name: "上下文详情" }));
  return { port, submit, queue, context, settings, send, card, host, gap: () => act(() => gap?.()), setRunning: (value: boolean) => { kernelRunning = value; } };
}

const draft = () => document.querySelector<HTMLTextAreaElement>('textarea[aria-label="任务输入"]')!;

async function confirm(card: ReturnType<typeof within>) {
  fireEvent.click(card.getByRole("button", { name: "立即压缩" }));
  fireEvent.click(card.getByRole("button", { name: "确认压缩" }));
}

it("asks before compacting from the ring, preserving the draft and avoiding a fake user turn", async () => {
  const pane = await mount();
  fireEvent.change(draft(), { target: { value: "Keep my unsent prompt" } });
  fireEvent.click(pane.card().getByRole("button", { name: "立即压缩" }));
  expect(pane.card().getByRole("alertdialog")).toBeTruthy();
  expect(pane.submit).not.toHaveBeenCalled();
  fireEvent.click(pane.card().getByRole("button", { name: "确认压缩" }));
  await waitFor(() => expect(pane.submit).toHaveBeenCalledExactlyOnceWith("/compact"));
  expect(draft().value).toBe("Keep my unsent prompt");
  expect(screen.queryByText("/compact")).toBeNull();
  expect(pane.queue).not.toHaveBeenCalled();
});

it.each(["cancel", "escape", "outside"])("dismisses confirmation with %s without submitting", async (way) => {
  const pane = await mount();
  fireEvent.click(pane.card().getByRole("button", { name: "立即压缩" }));
  if (way === "cancel") fireEvent.click(pane.card().getByRole("button", { name: "取消" }));
  else if (way === "escape") fireEvent.keyDown(pane.card().getByRole("alertdialog"), { key: "Escape" });
  else fireEvent.mouseDown(draft());
  expect(pane.card().queryByRole("alertdialog")).toBeNull();
  expect(pane.submit).not.toHaveBeenCalled();
});

it.each(["compacted", "compact_declined", "compact_failed"])("keeps both entry points disabled until the %s receipt", async (code) => {
  const pane = await mount(false, true);
  const rail = within(pane.host);
  await confirm(pane.card());
  await waitFor(() => expect(pane.submit).toHaveBeenCalledTimes(1));
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
  expect(rail.getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
  pane.send({ kind: "notice", code: "runtime_reloaded", text: "compacted" });
  expect(rail.getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
  pane.send({ kind: "notice", code, text: "kernel feedback", detail: "input_unchanged" });
  await waitFor(() => expect(rail.getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false);
  await screen.findByText(code === "compacted" ? "已压缩" : code === "compact_declined" ? "无需压缩：上下文自上次整理后没有变化" : "压缩失败：上下文自上次整理后没有变化");
});

it("compacts from the context panel through the same controller path", async () => {
  const pane = await mount(false, true);
  await confirm(within(pane.host));
  await waitFor(() => expect(pane.submit).toHaveBeenCalledExactlyOnceWith("/compact"));
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
});

it("disables manual compaction on a paired running session and on streamed compaction", async () => {
  const pane = await mount(true);
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(true);
  pane.setRunning(false);
  pane.send({ kind: "turn_done" });
  await waitFor(() => expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
  pane.send({ kind: "compaction_started", compaction: { trigger: "manual" } });
  await waitFor(() => expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(true));
  const before = pane.context.mock.calls.length;
  pane.send({ kind: "compaction_done", compaction: { trigger: "manual" } });
  await waitFor(() => expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
  await waitFor(() => expect(pane.context.mock.calls.length).toBeGreaterThan(before));
  expect(pane.submit).not.toHaveBeenCalled();
});

it("refuses a confirmation whose turn started while it was open", async () => {
  const pane = await mount();
  fireEvent.click(pane.card().getByRole("button", { name: "立即压缩" }));
  pane.setRunning(true);
  pane.send({ kind: "turn_started" });
  await waitFor(() => expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "确认压缩" }).disabled).toBe(true));
  fireEvent.click(pane.card().getByRole("button", { name: "确认压缩" }));
  expect(pane.submit).not.toHaveBeenCalled();
});

it("reports a transport refusal and restores the action for retry without queueing", async () => {
  const pane = await mount();
  pane.submit.mockRejectedValueOnce(new Error("transport refused"));
  await confirm(pane.card());
  await screen.findByText("transport refused");
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false);
  expect(pane.queue).not.toHaveBeenCalled();
  await confirm(pane.card());
  await waitFor(() => expect(pane.submit).toHaveBeenCalledTimes(2));
});

it("keeps the adjacent MCP management action working", async () => {
  const pane = await mount();
  fireEvent.click(pane.card().getByRole("button", { name: "管理 MCP 与工具" }));
  expect(pane.settings).toHaveBeenCalledExactlyOnceWith("ext");
});

it("holds both controls during HTTP admission and refuses a second confirmation", async () => {
  const pane = await mount(false, true);
  let admitted: (() => void) | undefined;
  pane.submit.mockImplementationOnce(() => new Promise<void>((resolve) => { admitted = resolve; }));
  fireEvent.click(pane.card().getByRole("button", { name: "立即压缩" }));
  fireEvent.click(within(pane.host).getByRole("button", { name: "立即压缩" }));
  fireEvent.click(within(pane.host).getByRole("button", { name: "确认压缩" }));
  expect(pane.submit).toHaveBeenCalledTimes(1);
  fireEvent.click(pane.card().getByRole("button", { name: "确认压缩" }));
  expect(pane.submit).toHaveBeenCalledTimes(1);
  await act(async () => admitted?.());
  expect(within(pane.host).getByRole<HTMLButtonElement>("button", { name: "正在压缩…" }).disabled).toBe(true);
  pane.send({ kind: "notice", code: "compacted", text: "compacted" });
  await waitFor(() => expect(within(pane.host).getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
});

it("does not rearm when a fast result arrives before HTTP admission resolves", async () => {
  const pane = await mount();
  let admitted: (() => void) | undefined;
  pane.submit.mockImplementationOnce(() => new Promise<void>((resolve) => { admitted = resolve; }));
  await confirm(pane.card());
  pane.send({ kind: "notice", code: "compact_declined", text: "nothing to compact", detail: "input_unchanged" });
  await act(async () => admitted?.());
  expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false);
});

it("releases a request whose result was lost across a stream gap", async () => {
  const pane = await mount();
  await confirm(pane.card());
  await waitFor(() => expect(pane.submit).toHaveBeenCalledTimes(1));
  pane.gap();
  await waitFor(() => expect(pane.card().getByRole<HTMLButtonElement>("button", { name: "立即压缩" }).disabled).toBe(false));
});
