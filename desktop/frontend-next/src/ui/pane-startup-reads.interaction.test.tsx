// @vitest-environment jsdom
import "./testkit";
import { useCallback, useState } from "react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import type { SessionStatus } from "../port/port";
import type { WireEvent } from "../port/wire";
import { Chrome } from "./Chrome";
import { Pane, type PaneReport } from "./Pane";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });
Element.prototype.getAnimations ??= () => [];

const deferredReads = ["mcp", "changes", "context", "queue"] as const;
const reads = ["checkpoints", "mcp", "changes", "context", "balance", "todos", "queue", "workspaces", "capabilityScope", "models"] as const;

class StartupPort extends MockPort {
  branchReads = vi.fn();
  async capabilityScope() {
    this.branchReads();
    return { ...await super.capabilityScope(), repo: true, branch: "rescue" };
  }
  async workspaceGit() {
    this.branchReads();
    return { repo: true, name: "workspace", branch: "rescue", detached: false, added: 0, removed: 0, untracked: 0 };
  }
}

async function mount(running = false, pathless = false) {
  const port = new StartupPort();
  const snapshot = await port.status();
  const mcp = await port.mcp();
  const sessionPath = pathless ? undefined : "/sessions/active.jsonl";
  let state: SessionStatus = { ...snapshot, running, sessionPath };
  let release: () => void = () => {};
  let reject: () => void = () => {};
  vi.spyOn(port, "status").mockImplementationOnce(() => new Promise((resolve, fail) => { release = () => resolve(state); reject = () => fail(new Error("status offline")); }))
    .mockImplementation(async () => state);
  const calls = Object.fromEntries(reads.map((name) => [name, vi.spyOn(port, name)])) as Record<typeof reads[number], ReturnType<typeof vi.spyOn>>;
  calls.mcp.mockResolvedValue(mcp);
  const history = vi.spyOn(port, "history");
  let deliver: (event: WireEvent) => void = () => {};
  let gap: () => void = () => {};
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap) => { deliver = onEvent; gap = onGap ?? (() => {}); return () => true; });
  function Window() {
    const [report, setReport] = useState<PaneReport | null>(null);
    const onReport = useCallback((_: string, next: PaneReport) => setReport(next), []);
    return <>
      <Chrome port={port} status={report?.status ?? null} title="active" steer={0} onSettings={() => {}} onBrowser={() => {}}
        browser={false} account={null} rail theme="light" onRail={() => {}} onTheme={() => {}} onFind={() => {}} />
      <Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: "/workspace", name: "workspace", sessionPath }}
        title="active" active visible sideHost={null} side={false} onFocus={() => {}} onReport={onReport}
        onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
        onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />
    </>;
  }
  const view = render(<Window />);
  await waitFor(() => expect(history).toHaveBeenCalledTimes(1));
  return { port, calls, history, view, ready: () => act(async () => release()), fail: () => act(async () => reject()),
    send: (event: WireEvent) => act(() => deliver(event)), gap: () => act(() => gap()),
    change: (next: Partial<SessionStatus>) => { state = { ...state, ...next }; } };
}

it.each([false, true])("reads each deferred startup resource once after the session is known (running=%s)", async (running) => {
  const pane = await mount(running);
  await pane.ready();
  await waitFor(() => expect(pane.calls.workspaces.mock.calls.length).toBeGreaterThan(0));
  for (const name of deferredReads) expect(pane.calls[name], name).toHaveBeenCalledTimes(1);
  expect(pane.calls.balance).toHaveBeenCalledTimes(1);
  expect(pane.calls.todos).toHaveBeenCalledTimes(1);
  expect(pane.calls.models).toHaveBeenCalledTimes(1);
});

it("waits for the initial status before reading deferred panels without holding up history", async () => {
  const pane = await mount();
  expect(pane.history).toHaveBeenCalledTimes(1);
  for (const name of deferredReads) expect(pane.calls[name], name).not.toHaveBeenCalled();
  await pane.ready();
  await screen.findByRole("button", { name: "查看上下文与压缩" });
});

it("reads once for a new session which has no persisted path yet", async () => {
  const pane = await mount(false, true);
  await pane.ready();
  await waitFor(() => expect(pane.calls.workspaces.mock.calls.length).toBeGreaterThan(0));
  for (const name of deferredReads) expect(pane.calls[name], name).toHaveBeenCalledTimes(1);
});

it("refreshes after turn boundaries, a completed write, compaction and an MCP handshake", async () => {
  const pane = await mount();
  await pane.ready();
  await waitFor(() => expect(pane.calls.workspaces.mock.calls.length).toBeGreaterThan(0));
  const before = Object.fromEntries(reads.map((name) => [name, pane.calls[name].mock.calls.length]));
  pane.change({ running: true });
  pane.send({ kind: "turn_started" });
  await waitFor(() => expect(pane.calls.checkpoints.mock.calls.length).toBeGreaterThan(before.checkpoints!));
  const wrote = pane.calls.changes.mock.calls.length;
  pane.send({ kind: "tool_dispatch", tool: { id: "write", name: "write_file", args: "{}", readOnly: false } });
  pane.send({ kind: "tool_result", tool: { id: "write", name: "write_file", args: "{}", readOnly: false, output: "saved" } });
  await waitFor(() => expect(pane.calls.changes.mock.calls.length).toBeGreaterThan(wrote));
  const folded = pane.calls.context.mock.calls.length;
  pane.send({ kind: "compaction_started" });
  pane.send({ kind: "compaction_done" });
  await waitFor(() => expect(pane.calls.context.mock.calls.length).toBeGreaterThan(folded));
  const connected = pane.calls.mcp.mock.calls.length;
  pane.send({ kind: "mcp_surface_ready" });
  await waitFor(() => expect(pane.calls.mcp.mock.calls.length).toBeGreaterThan(connected));
  pane.change({ running: false });
  pane.send({ kind: "turn_done" });
  await waitFor(() => expect(pane.calls.balance.mock.calls.length).toBeGreaterThan(before.balance!));
  expect(pane.calls.todos.mock.calls.length).toBeGreaterThan(before.todos!);
});

it("refreshes the session and workspace on recovery without rereading models", async () => {
  const pane = await mount();
  await pane.ready();
  await waitFor(() => expect(pane.calls.workspaces.mock.calls.length).toBeGreaterThan(0));
  const before = Object.fromEntries(reads.map((name) => [name, pane.calls[name].mock.calls.length]));
  const branchReads = pane.port.branchReads.mock.calls.length;
  pane.change({ sessionPath: "/sessions/next.jsonl", workspaceRoot: "/next" });
  pane.gap();
  await waitFor(() => expect(pane.calls.workspaces.mock.calls.length).toBeGreaterThan(before.workspaces!));
  for (const name of ["checkpoints", "mcp", "changes", "context", "queue"] as const) {
    expect(pane.calls[name].mock.calls.length, name).toBeGreaterThan(before[name]!);
  }
  expect(pane.calls.models).toHaveBeenCalledTimes(1);
  expect(pane.history).toHaveBeenCalledTimes(2);
  expect(pane.port.branchReads.mock.calls.length).toBeGreaterThan(branchReads);
});

it("loads deferred panels and the branch chip when the first status fails", async () => {
  const pane = await mount();
  await pane.fail();
  await waitFor(() => expect(pane.calls.queue).toHaveBeenCalledTimes(1));
  for (const name of deferredReads) expect(pane.calls[name], name).toHaveBeenCalledTimes(1);
  expect(pane.port.branchReads).toHaveBeenCalled();
  expect(pane.calls.changes).toHaveBeenCalled();
  await screen.findByLabelText("当前 Git 分支：rescue");
  pane.send({ kind: "turn_done" });
  await waitFor(() => expect(pane.calls.queue).toHaveBeenCalledTimes(2));
  expect(pane.calls.models).toHaveBeenCalledTimes(1);
});

it("keeps event-driven reads alive while the first status is pending", async () => {
  const pane = await mount();
  pane.send({ kind: "inbox_changed" });
  await waitFor(() => expect(pane.calls.queue).toHaveBeenCalledTimes(1));
  pane.send({ kind: "tool_dispatch", tool: { id: "write", name: "write_file", args: "{}", readOnly: false } });
  pane.send({ kind: "tool_result", tool: { id: "write", name: "write_file", args: "{}", readOnly: false, output: "saved" } });
  await waitFor(() => expect(pane.calls.changes).toHaveBeenCalledTimes(1));
  pane.send({ kind: "compaction_started" });
  pane.send({ kind: "compaction_done" });
  await waitFor(() => expect(pane.calls.context).toHaveBeenCalledTimes(1));
  pane.send({ kind: "mcp_surface_ready" });
  await waitFor(() => expect(pane.calls.mcp).toHaveBeenCalledTimes(1));
  await pane.ready();
  await waitFor(() => expect(pane.calls.queue).toHaveBeenCalledTimes(2));
});
