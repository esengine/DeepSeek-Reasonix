// @vitest-environment jsdom
import "./testkit";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { RuntimeView } from "../port/hub";
import type { AgentPort } from "../port/port";
import { useOpenPane } from "./useOpenPane";
import { setOpenSessionLimit } from "../state/prefs";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

function localOpen(remote: RuntimeView) {
  const open = vi.fn().mockResolvedValue({ id: "local", root: "/w", name: "w" });
  const close = vi.fn().mockResolvedValue(undefined);
  const resume = vi.fn().mockResolvedValue(undefined);
  const setActive = vi.fn();
  const { result } = renderHook(() => useOpenPane({ hub: { open, close } as never,
    runtimes: [{ id: "current", base: "", root: "/other", name: "other", sessionPath: "/s/current" }, remote],
    panePorts: new Map([[remote.id, { resume } as unknown as AgentPort]]), active: "current",
    reportsRef: { current: {} }, runsRef: { current: {} }, setRuntimes: vi.fn(), setActive, setTakeover: vi.fn(),
    reloadPanes: async () => {}, sw: { path: "", pending: false, opening: () => false, begin: () => 1,
      current: () => true, landed: vi.fn(), end: vi.fn(), cancel: vi.fn() } }));
  return { open, close, resume, setActive, run: (sessionPath?: string) => act(async () => { await result.current({ root: "/w", sessionPath }); }) };
}

const remote = (sessionPath?: string): RuntimeView => ({ id: "remote", base: "/r/remote", host: "gpu", root: "/w", name: "w", sessionPath });

it("opens a local new conversation instead of focusing a blank SSH pane with the same path", async () => {
  const flow = localOpen(remote());
  await flow.run();
  expect(flow.open).toHaveBeenCalledWith({ root: "/w", sessionPath: undefined });
  expect(flow.setActive).toHaveBeenCalledWith("local");
  expect(flow.close).not.toHaveBeenCalled();
});

it("does not resume a local transcript into a blank SSH pane", async () => {
  const flow = localOpen(remote());
  await flow.run("/w/s/history");
  expect(flow.resume).not.toHaveBeenCalled();
  expect(flow.open).toHaveBeenCalledWith({ root: "/w", sessionPath: "/w/s/history" });
  expect(flow.close).not.toHaveBeenCalled();
});

it("does not reuse an SSH pane for a local transcript at the tab limit", async () => {
  setOpenSessionLimit(2);
  const flow = localOpen(remote("/w/s/remote"));
  await flow.run("/w/s/local");
  expect(flow.resume).not.toHaveBeenCalled();
  expect(flow.open).toHaveBeenCalledWith({ root: "/w", sessionPath: "/w/s/local" });
});

function remoteWindow() {
  const hub = new MockHub();
  const gates: Array<() => Promise<void>> = [];
  const open = hub.openRemote.bind(hub);
  hub.openRemote = (req) => new Promise<RuntimeView>((resolve) => gates.push(async () => { resolve(await open(req)); }));
  const portFor = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = portFor(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  render(<App hub={hub} />);
  return { gates, hub };
}

it("keeps the latest SSH session selected when an earlier connection finishes late", async () => {
  const { gates } = remoteWindow();
  const first = await screen.findByRole("treeitem", { name: /数据管线/ });
  const second = await screen.findByRole("treeitem", { name: /跑一遍评测/ });
  await userEvent.click(first);
  await userEvent.click(second);
  expect(gates).toHaveLength(2);
  await act(async () => { await gates[1](); });
  await waitFor(() => expect(second.getAttribute("aria-selected")).toBe("true"));
  await act(async () => { await gates[0](); });
  await waitFor(() => expect(document.querySelectorAll(".pane")).toHaveLength(3));
  expect(second.getAttribute("aria-selected")).toBe("true");
  expect(first.getAttribute("aria-selected")).toBe("false");
});

it("does not steal focus back from a local conversation when an SSH open finishes late", async () => {
  const { gates } = remoteWindow();
  const local = await screen.findByRole("treeitem", { name: /并行会话演示/ });
  const target = await screen.findByRole("treeitem", { name: /数据管线/ });
  await userEvent.click(target);
  await userEvent.click(local);
  await act(async () => { await gates[0](); });
  await waitFor(() => expect(document.querySelectorAll(".pane")).toHaveLength(2));
  expect(local.getAttribute("aria-selected")).toBe("true");
  expect(target.getAttribute("aria-selected")).toBe("false");
});

it("does not let an older local open replace the newer SSH selection", async () => {
  const { hub, gates } = remoteWindow();
  const open = hub.open.bind(hub);
  let finish!: () => Promise<void>;
  hub.open = (req) => new Promise<RuntimeView>((resolve) => { finish = async () => { resolve(await open(req)); }; });
  const local = await screen.findByRole("treeitem", { name: /上一次的会话/ });
  const target = await screen.findByRole("treeitem", { name: /数据管线/ });
  await userEvent.click(local);
  await userEvent.click(target);
  await act(async () => { await finish(); });
  expect(local.getAttribute("aria-selected")).toBe("false");
  expect(document.querySelector(".panes")?.getAttribute("aria-busy")).toBe("true");
  await act(async () => { await gates[0](); });
  await waitFor(() => expect(target.getAttribute("aria-selected")).toBe("true"));
  expect(document.querySelector(".panes")?.getAttribute("aria-busy")).toBeNull();
});

it("shares a pending SSH session open across repeated clicks and a focus detour", async () => {
  const { gates } = remoteWindow();
  const local = await screen.findByRole("treeitem", { name: /并行会话演示/ });
  const target = await screen.findByRole("treeitem", { name: /数据管线/ });
  await userEvent.click(target);
  await userEvent.click(local);
  await userEvent.click(target);
  expect(gates).toHaveLength(1);
  await act(async () => { await gates[0](); });
  await waitFor(() => expect(target.getAttribute("aria-selected")).toBe("true"));
  expect(document.querySelectorAll(".pane")).toHaveLength(2);
});

it("names an existing SSH pane from the remote history instead of calling it a new conversation", async () => {
  const { gates } = remoteWindow();
  await userEvent.click(await screen.findByRole("treeitem", { name: /数据管线/ }));
  await act(async () => { await gates[0](); });
  await waitFor(() => expect(document.querySelector('.pane[data-active]')?.getAttribute('aria-label')).toBe('数据管线'));
});
