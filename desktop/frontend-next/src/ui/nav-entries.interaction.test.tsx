// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";
import { Nav } from "./Nav";
import { Sidebar } from "./Sidebar";
import { NO_PAGING } from "./treewindow";
import { NavEntriesSection } from "./NavEntriesSection";

const props = { at: null, onGo: vi.fn(), onFiles: vi.fn(), onHome: vi.fn(), onRail: vi.fn(), onFeedback: vi.fn(), onNew: vi.fn(), feedbackUnread: 0, shown: true };

beforeEach(() => localStorage.clear());
afterEach(cleanup);

const rail = () => [...document.querySelectorAll(".nav .navgrp:not(.foot) button")].map((b) => b.getAttribute("aria-label"));

function both() {
  render(<><NavEntriesSection /><Nav {...props} /></>);
}

it("draws the rail in the declared order until this machine chooses otherwise", () => {
  both();
  expect(rail()).toEqual(["新建会话", "会话", "用量", "文件", "工具与权限", "扩展", "记忆", "远程"]);
});

it("hides a destination from the rail when its switch is turned off, and brings it back", async () => {
  both();
  await userEvent.click(screen.getByRole("switch", { name: "显示：记忆" }));
  expect(rail()).not.toContain("记忆");
  expect(screen.getByRole("switch", { name: "显示：记忆" }).getAttribute("aria-checked")).toBe("false");
  await userEvent.click(screen.getByRole("switch", { name: "显示：记忆" }));
  expect(rail()).toContain("记忆");
});

it("moves a destination up and down, and the rail follows", async () => {
  both();
  await userEvent.click(screen.getByRole("button", { name: "上移：远程" }));
  await userEvent.click(screen.getByRole("button", { name: "上移：远程" }));
  expect(rail()).toEqual(["新建会话", "会话", "用量", "文件", "工具与权限", "远程", "扩展", "记忆"]);
  await userEvent.click(screen.getByRole("button", { name: "下移：远程" }));
  expect(rail().slice(-3)).toEqual(["扩展", "远程", "记忆"]);
});

it("cannot move the first entry up or the last one down", () => {
  both();
  expect((screen.getByRole("button", { name: "上移：用量" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: "下移：远程" }) as HTMLButtonElement).disabled).toBe(true);
});

it("restores the arrangement, and offers that only after a change", async () => {
  both();
  const reset = screen.getByRole("button", { name: "还原排列" }) as HTMLButtonElement;
  expect(reset.disabled).toBe(true);
  await userEvent.click(screen.getByRole("switch", { name: "显示：文件" }));
  expect(reset.disabled).toBe(false);
  await userEvent.click(reset);
  expect(rail()).toContain("文件");
  expect(reset.disabled).toBe(true);
});

const tree: TreeWorkspace[] = [{ root: "/work/alpha", name: "alpha", remembered: true, sessions: [] }];

function sidebar() {
  const noop = vi.fn();
  render(
    <Sidebar
      onFiles={noop} onView={noop} hub={new MockHub()} collapsed={false} navShown={false} paging={NO_PAGING} tree={tree} treeRead runtimes={[]} runs={{}} active=""
      activeWorkspace={tree[0]} liveIds={() => []} pinned={new Set()} onPin={noop} remotes={null} remoteTrees={{}}
      reloadRemotes={async () => {}} reloadRemoteTrees={async () => {}} readRemoteTree={async () => {}} reloadTree={async () => {}}
      adder={{ busy: false, add: noop, addPath: noop } as never} onOpen={async () => {}} onOpenRemote={async () => {}} onFocusPane={noop} onClosePanes={async () => {}}
      onPause={noop} onArchive={async () => {}} onRename={noop} onCollapse={noop} account={null} accountUnread="" wallet=""
      onSettings={noop} onFeedback={noop} feedbackUnread={0} onFind={noop} onError={noop}
    />,
  );
}

const shortcuts = () => [...document.querySelectorAll(".studio-quicknav > button")].map((b) => b.textContent);

it("gives the sidebar the same order and the same hidden entries as the rail", async () => {
  render(<NavEntriesSection />);
  sidebar();
  expect(shortcuts()).toEqual(["文件", "工具与权限", "扩展", "记忆", "远程"]);
  await userEvent.click(screen.getByRole("button", { name: "上移：远程" }));
  await userEvent.click(screen.getByRole("button", { name: "上移：远程" }));
  await userEvent.click(screen.getByRole("switch", { name: "显示：记忆" }));
  expect(shortcuts()).toEqual(["文件", "工具与权限", "远程", "扩展"]);
});

it("takes the wallet row out of the sidebar when Usage is hidden", async () => {
  render(<NavEntriesSection />);
  sidebar();
  expect(document.querySelector(".studio-wallet:not(.studio-feedback)")).not.toBeNull();
  await userEvent.click(screen.getByRole("switch", { name: "显示：用量" }));
  expect(document.querySelector(".studio-wallet:not(.studio-feedback)")).toBeNull();
});
