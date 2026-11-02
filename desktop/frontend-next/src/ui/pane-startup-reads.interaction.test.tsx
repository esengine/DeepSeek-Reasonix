// @vitest-environment jsdom
import "./testkit";
import { useCallback, useState } from "react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { SessionStatus, WorkspaceInfo } from "../port/port";
import type { WireEvent } from "../port/wire";
import { Chrome } from "./Chrome";
import { Pane, type PaneReport } from "./Pane";
import { App } from "./App";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });
Element.prototype.getAnimations ??= () => [];

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

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
  const sources = port.worktreeSources.bind(port);
  vi.spyOn(port, "worktreeSources").mockImplementation(async () => ({ ...await sources(), sourceRoot: state.workspaceRoot || state.cwd }));
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
      <Chrome port={port} status={report?.status ?? null} workspace={report?.workspace ?? null} title="active" steer={0} account={null} theme="light" onTheme={() => {}} onFind={() => {}} view="flow" rows={0} surfaces={0} onView={() => {}} />
      <Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: snapshot.workspaceRoot!, name: "workspace", sessionPath }}
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

it.each([
  [false, false], [true, false], [false, true], [true, true],
])("reads startup resources once (running=%s, pathless=%s)", async (running, pathless) => {
  const pane = await mount(running, pathless);
  expect(pane.calls.workspaces).not.toHaveBeenCalled();
  await pane.ready();
  await waitFor(() => expect(pane.calls.workspaces).toHaveBeenCalledTimes(1));
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
  await waitFor(() => expect(pane.calls.workspaces).toHaveBeenCalledTimes(1));
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
  expect((await screen.findByRole("button", { name: "配置工作目录" })).textContent).toContain("rescue");
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(1);
  pane.send({ kind: "turn_done" });
  await waitFor(() => expect(pane.calls.queue).toHaveBeenCalledTimes(2));
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(1);
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

it("shares the workspace result with both directory badges and rejects a stale root read", async () => {
  const pane = await mount();
  const old = deferred<WorkspaceInfo>();
  pane.calls.workspaces.mockReturnValueOnce(old.promise);
  await pane.ready();
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(1);
  pane.calls.workspaces.mockResolvedValue({ current: "/next", isolated: true, canSwitch: true, canIsolate: true, recents: [] });
  pane.change({ workspaceRoot: "/next" });
  pane.gap();
  await waitFor(() => expect(document.querySelector(".chrome .isolab")?.getAttribute("title")).toContain("/next"));
  await waitFor(() => expect(screen.getByRole("button", { name: "配置工作目录" }).textContent).toContain("独立工作树"));
  await act(async () => old.resolve({ current: "/old", isolated: true, canSwitch: true, canIsolate: true, recents: [] }));
  expect(document.querySelector(".chrome .isolab")?.getAttribute("title")).toContain("/next");
  expect(screen.getByRole("button", { name: "配置工作目录" }).title).toContain("/next");
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(2);
});

it("recovers workspace metadata after a failed read when the directory changes", async () => {
  const pane = await mount();
  pane.calls.workspaces.mockRejectedValueOnce(new Error("workspace offline"));
  await pane.ready();
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(1);
  expect(document.querySelector(".chrome .isolab:not(.hostlab)")).toBeNull();
  pane.calls.workspaces.mockResolvedValue({ current: "/next", isolated: true, canSwitch: true, canIsolate: true, recents: [] });
  pane.change({ workspaceRoot: "/next" });
  pane.gap();
  await waitFor(() => expect(document.querySelector(".chrome .isolab")?.getAttribute("title")).toContain("/next"));
  expect(pane.calls.workspaces).toHaveBeenCalledTimes(2);
});

it("keeps workspace reads and late results isolated across focused panes", async () => {
  const hub = new MockHub();
  const portFor = hub.portFor.bind(hub);
  const calls = new Map<string, number>();
  const first = deferred<WorkspaceInfo>();
  let firstRoot = "";
  hub.portFor = (rt) => {
    const port = portFor(rt);
    if (calls.has(rt.id)) return port;
    calls.set(rt.id, 0);
    const status = port.status.bind(port);
    port.status = async () => ({ ...await status(), workspaceRoot: rt.root, sessionPath: rt.sessionPath });
    port.welcomeSeen = async () => true;
    port.providerSetup = async () => null;
    port.workspaces = () => {
      calls.set(rt.id, calls.get(rt.id)! + 1);
      if (rt.id === "r1") { firstRoot = rt.root; return first.promise; }
      return Promise.resolve({ current: rt.root, isolated: false, canSwitch: true, canIsolate: true, recents: [] });
    };
    return port;
  };
  render(<App hub={hub} />);
  await waitFor(() => expect(calls.get("r1")).toBe(1));
  await userEvent.click(await screen.findByRole("treeitem", { name: /站点改版/ }));
  await waitFor(() => expect([...calls.values()]).toEqual([1, 1]));
  await act(async () => first.resolve({ current: firstRoot, isolated: true, canSwitch: true, canIsolate: true, recents: [] }));
  expect(document.querySelector(".chrome .isolab:not(.hostlab)")).toBeNull();
  await userEvent.click(screen.getByRole("treeitem", { name: /并行会话演示/ }));
  await waitFor(() => expect(document.querySelector(".chrome .isolab")?.getAttribute("title")).toContain(firstRoot));
  expect([...calls.values()]).toEqual([1, 1]);
});
