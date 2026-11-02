// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { RecentSessions } from "./RecentSessions";
import { MockHub } from "../port/mock_hub";
import type { TreeWindow, TreeWorkspace } from "../port/hub";
import type { WireEvent } from "../port/wire";
import { setHidesLegacySessions, setLiveGraceMinutes } from "../state/prefs";

afterEach(() => { cleanup(); setHidesLegacySessions(false); setLiveGraceMinutes(0); localStorage.clear(); });

function setup() {
  const hub = new MockHub();
  const portFor = hub.portFor.bind(hub);
  hub.portFor = (runtime) => {
    const port = portFor(runtime);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  hub.remoteHosts = async () => [];
  return hub;
}

it("pages the complete recent history, returning to the previous page and retrying a failed read", async () => {
  const hub = setup();
  const asked: TreeWindow[] = [];
  let fail = false;
  hub.tree = async (window) => {
    asked.push(window!);
    if (fail) { fail = false; throw new Error("offline"); }
    const offset = window?.offset ?? 0;
    return [{ root: "/w", name: "w", recentOffset: offset, remaining: Math.max(0, 351 - offset - 50), sessions: Array.from({ length: Math.min(50, 351 - offset) }, (_, i) => ({
      path: `/w/${offset + i}.jsonl`, name: `finished ${offset + i}`, lastActiveAt: 1000 - offset - i,
    })) }];
  };
  render(<RecentSessions hub={hub} hosts={[]} onPick={async () => {}} onClose={() => {}} />);
  const dialog = within(screen.getByRole("dialog", { name: "最近的会话" }));
  expect(await dialog.findByRole("button", { name: /finished 0 · w/ })).toBeTruthy();
  for (let page = 1; page <= 6; page++) await userEvent.click(dialog.getByRole("button", { name: "下一页" }));
  expect(await dialog.findByRole("button", { name: /finished 300 · w/ })).toBeTruthy();
  fail = true;
  await userEvent.click(dialog.getByRole("button", { name: "下一页" }));
  expect(await dialog.findByRole("alert")).toHaveProperty("textContent", "offline");
  await userEvent.click(dialog.getByRole("button", { name: "重试" }));
  expect(await dialog.findByRole("button", { name: /finished 350 · w/ })).toBeTruthy();
  expect(dialog.getByRole("button", { name: "下一页" })).toHaveProperty("disabled", true);
  await userEvent.click(dialog.getByRole("button", { name: "上一页" }));
  expect(await dialog.findByRole("button", { name: /finished 300 · w/ })).toBeTruthy();
  expect(asked.at(-1)).toMatchObject({ order: "recent", scope: "active", limit: 50, offset: 300 });
});

it("discovers a finished conversation and reveals its collapsed project section", async () => {
  const hub = setup();
  const ordinary = hub.tree.bind(hub);
  const open = vi.spyOn(hub, "open");
  const root = "~/projects/my-website";
  const path = "/sessions/finished.jsonl";
  hub.tree = async (window) => {
    if (window?.order === "recent") return [{ root, name: "my-website", recentOffset: window.offset ?? 0, remaining: 0, sessions: [{ path, name: "finished", title: "Finished release", lastActiveAt: 2000 }] }];
    const tree = await ordinary(window);
    const held = (await hub.runtimes()).find((rt) => rt.sessionPath === path);
    return tree.map((ws) => ws.root !== root || !held ? ws : { ...ws, sessions: [...ws.sessions, { path, name: "finished", title: "Finished release", runtimeId: held.id }] });
  };
  render(<App hub={hub} />);
  const project = await screen.findByRole("treeitem", { name: /my-website/ });
  await userEvent.click(project);
  await userEvent.click(screen.getByRole("button", { name: "收起项目" }));
  await userEvent.click(screen.getByRole("button", { name: "最近的会话" }));
  await userEvent.click(await screen.findByRole("button", { name: "Finished release · my-website" }));
  await waitFor(() => expect(open).toHaveBeenCalledWith(expect.objectContaining({ root, sessionPath: path })));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "最近的会话" })).toBeNull());
  const located = await screen.findByRole("treeitem", { name: "Finished release" });
  expect(located.closest(".wsnode")?.getAttribute("data-current")).toBe("");
  expect(project.getAttribute("aria-expanded")).toBe("true");
  expect(located.getAttribute("aria-selected")).toBe("true");
  expect(document.activeElement).toBe(located);
});

it("focuses an already-open session without opening it again, and resets Running and sidebar search", async () => {
  const hub = setup();
  const ordinary = hub.tree.bind(hub);
  hub.tree = async (window) => window?.order === "recent" ? [{ root: "~/projects/DeepSeek-Reasonix", name: "DeepSeek-Reasonix", recentOffset: window.offset ?? 0, remaining: 0, sessions: [{
    path: "/sessions/mock.jsonl", name: "mock", title: "Recent finished", runtimeId: "r1", lastActiveAt: 2000,
  }] }] : ordinary(window);
  const open = vi.spyOn(hub, "open");
  render(<App hub={hub} />);
  await screen.findByRole("treeitem", { name: "并行会话演示" });
  const segments = document.querySelector(".studio-session-segments")!;
  await userEvent.click(within(segments as HTMLElement).getByRole("button", { name: "进行中" }));
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "unrelated");
  await userEvent.click(screen.getByRole("button", { name: "最近的会话" }));
  await userEvent.click(await screen.findByRole("button", { name: "Recent finished · DeepSeek-Reasonix" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "最近的会话" })).toBeNull());
  expect(open).not.toHaveBeenCalled();
  expect(within(segments as HTMLElement).getByRole("button", { name: "全部" }).getAttribute("aria-pressed")).toBe("true");
  expect(screen.getByRole("searchbox", { name: "搜索会话 / 项目" })).toHaveProperty("value", "");
  expect(await screen.findByRole("treeitem", { name: "并行会话演示" })).toBeTruthy();
});

it("reads remote recent pages only after selecting that machine, retaining host-qualified identity", async () => {
  const hub = setup();
  const remote = vi.spyOn(hub, "remoteTree").mockResolvedValue([{ root: "/same", name: "remote project", recentOffset: 0, remaining: 0, sessions: [{ path: "/same/session.jsonl", name: "Remote finished", workspaceRoot: "/same/worktree", lastActiveAt: 5000 }] }]);
  const picked = vi.fn(async () => {});
  render(<RecentSessions hub={hub} hosts={[{ name: "gpu-box", target: "mock", status: "stopped" }, { name: "disabled", target: "mock", status: "idle", disabled: true }]} onPick={picked} onClose={() => {}} />);
  expect(remote).not.toHaveBeenCalled();
  expect(screen.queryByRole("option", { name: "disabled" })).toBeNull();
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "运行环境" }), "gpu-box");
  await userEvent.click(await screen.findByRole("button", { name: "Remote finished · remote project" }));
  expect(remote).toHaveBeenCalledWith("gpu-box", expect.objectContaining({ order: "recent", offset: 0 }));
  expect(picked).toHaveBeenCalledWith("gpu-box", expect.objectContaining({ root: "/same" }), expect.objectContaining({ workspaceRoot: "/same/worktree" }));
});

it("keeps a just-completed session discoverable after it leaves Running", async () => {
  const hub = setup();
  const port = hub.portFor((await hub.runtimes())[0]);
  const snapshot = await port.status();
  let running = true;
  vi.spyOn(port, "status").mockImplementation(async () => ({ ...snapshot, running }));
  let deliver: ((event: WireEvent) => void) | undefined;
  vi.spyOn(port, "subscribe").mockImplementation((listener) => { deliver = listener; return () => {}; });
  setLiveGraceMinutes(0);
  render(<App hub={hub} />);
  await screen.findByRole("treeitem", { name: "并行会话演示" });
  await waitFor(() => expect(document.querySelector(".studio-runstate[data-running]")).not.toBeNull());
  const segments = within(document.querySelector(".studio-session-segments") as HTMLElement);
  await userEvent.click(segments.getByRole("button", { name: "进行中" }));
  expect(screen.getByRole("treeitem", { name: "并行会话演示" })).toBeTruthy();
  running = false;
  act(() => deliver?.({ kind: "turn_done" }));
  await waitFor(() => expect(screen.queryByRole("treeitem", { name: "并行会话演示" })).toBeNull());
  await userEvent.click(screen.getByRole("button", { name: "最近的会话" }));
  await userEvent.click(await screen.findByRole("button", { name: "并行会话演示 · DeepSeek-Reasonix" }));
  expect(await screen.findByRole("treeitem", { name: "并行会话演示" })).toBeTruthy();
  expect(segments.getByRole("button", { name: "全部" }).getAttribute("aria-pressed")).toBe("true");
});

it("uses activity rather than unread or pin state, and applies search before paging with the saved legacy preference", async () => {
  const fixture: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [
    { path: "/w/unread", name: "Unread older", unread: true, lastActiveAt: 100 },
    { path: "/w/pinned", name: "Pinned oldest", pinned: true, lastActiveAt: 50 },
    { path: "/w/recent", name: "Finished newest", lastActiveAt: 500 },
    { path: "/w/archive", name: "Archived hidden", archived: true, lastActiveAt: 1000 },
    { path: "/w/legacy", name: "Legacy history", legacy: true, lastActiveAt: 700 },
  ] }];
  class FixtureHub extends MockHub {
    tree(window?: TreeWindow) { return Promise.resolve(this.recentTree(fixture, window)); }
  }
  const hub = new FixtureHub();
  const asked = vi.spyOn(hub, "tree");
  setHidesLegacySessions(true);
  const view = render(<RecentSessions hub={hub} hosts={[]} onPick={async () => {}} onClose={() => {}} />);
  await screen.findByRole("button", { name: "Finished newest · w" });
  expect([...document.querySelectorAll(".recent-session")].map((row) => row.getAttribute("aria-label"))).toEqual(["Finished newest · w", "Unread older · w", "Pinned oldest · w"]);
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索最近会话 / 项目" }), "Legacy");
  expect(await screen.findByRole("button", { name: "Legacy history · w" })).toBeTruthy();
  expect(asked).toHaveBeenLastCalledWith(expect.objectContaining({ q: "Legacy", hideLegacy: true, offset: 0 }));
  view.unmount();
  render(<RecentSessions hub={hub} hosts={[]} onPick={async () => {}} onClose={() => {}} />);
  expect(await screen.findByRole("button", { name: "Finished newest · w" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Legacy history/ })).toBeNull();
});

it("does not mistake an older remote kernel's partial tree for complete recent history", async () => {
  const hub = setup();
  hub.remoteTree = async () => [{ root: "/w", name: "w", remaining: 100, sessions: [{ path: "/w/old", name: "Not a recent page" }] }];
  render(<RecentSessions hub={hub} hosts={[{ name: "old-host", target: "mock", status: "idle" }]} onPick={async () => {}} onClose={() => {}} />);
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "运行环境" }), "old-host");
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", "此运行环境不支持最近会话，请更新 Reasonix");
  expect(screen.queryByRole("button", { name: /Not a recent page/ })).toBeNull();
});

it("keeps failed opens in the picker and reconciles a removed session on retry", async () => {
  const hub = setup();
  let removed = false;
  hub.tree = async () => [{ root: "/w", name: "w", recentOffset: 0, remaining: 0, sessions: removed ? [] : [{ path: "/w/gone", name: "Removed elsewhere" }] }];
  const pick = vi.fn(async () => { removed = true; throw new Error("No longer available"); });
  const close = vi.fn();
  render(<RecentSessions hub={hub} hosts={[]} onPick={pick} onClose={close} />);
  await userEvent.click(await screen.findByRole("button", { name: "Removed elsewhere · w" }));
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", "No longer available");
  expect(close).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(await screen.findByText("没有匹配的会话")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Removed elsewhere · w" })).toBeNull();
});
