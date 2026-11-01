// @vitest-environment jsdom
import "./testkit";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

it("offers session management from the project list menu and hands focus back on close", async () => {
  const onManage = vi.fn();
  const props = {
    hub: { removeSession: vi.fn() } as never,
    tree: [{ root: "/w", name: "Project", sessions: [{ path: "/s/a.jsonl", name: "a", title: "Alpha" }] }] as TreeWorkspace[],
    treeRead: true, runtimes: [], active: "", folded: new Set<string>(), onFold: vi.fn(), reload: vi.fn().mockResolvedValue(undefined),
    onOpen: vi.fn(), onFocus: vi.fn(), onClose: vi.fn().mockResolvedValue(undefined), liveIds: () => [], runs: {},
    onRename: vi.fn(), onError: vi.fn(), adder: { add: vi.fn(), close: vi.fn(), at: null } as never, onManage,
  };
  render(<RailSearch><Workspaces {...props} /></RailSearch>);
  const more = screen.getByRole("button", { name: "项目列表的更多操作" });
  await userEvent.click(more);
  await userEvent.click(screen.getByRole("menuitem", { name: "管理会话" }));
  expect(onManage).toHaveBeenCalledOnce();
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(more);
});

it("opens the sheet from the sidebar and closes it with Escape", async () => {
  const hub = new MockHub();
  const portFor = hub.portFor.bind(hub);
  hub.portFor = (runtime) => {
    const port = portFor(runtime);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  hub.remoteHosts = async () => [];
  render(<App hub={hub} />);

  await userEvent.click(await screen.findByRole("button", { name: "项目列表的更多操作" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "管理会话" }));
  await screen.findByRole("dialog", { name: "管理会话" });
  expect(await screen.findByRole("checkbox", { name: "选择会话：并行会话演示" })).toBeTruthy();
  await userEvent.click(screen.getByRole("checkbox", { name: "选择会话：并行会话演示" }));
  const sheet = within(screen.getByRole("dialog", { name: "管理会话" }));
  expect(sheet.getByText("已选 1 个会话")).toBeTruthy();
  await userEvent.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "管理会话" })).toBeNull());
});
