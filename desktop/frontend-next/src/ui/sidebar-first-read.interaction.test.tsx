// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

function hubWithHeldTree() {
  const hub = new MockHub();
  hub.remoteHosts = async () => [];
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  let answer: (tree: TreeWorkspace[]) => void = () => {};
  hub.tree = () => new Promise<TreeWorkspace[]>((resolve) => (answer = resolve));
  return { hub, answer: (tree: TreeWorkspace[]) => answer(tree) };
}

// Before the kernel has answered, an empty tree is an unread one, and saying
// "no folders" there reads as data that was lost.
it("says the folders are being read until the first tree read settles", async () => {
  const { hub, answer } = hubWithHeldTree();
  render(<App hub={hub} />);

  expect(await screen.findByText("正在读取文件夹…")).toBeTruthy();
  expect(screen.queryByText("尚无文件夹")).toBeNull();
  expect(screen.queryByText("先打开一个项目")).toBeNull();

  await act(async () => answer([]));

  expect(await screen.findByText("尚无文件夹")).toBeTruthy();
  expect(screen.queryByText("正在读取文件夹…")).toBeNull();
});

// With no pane open the main area speaks for the folders too; before the read
// settles it cannot know there are none, so it offers no folder to add.
it("holds the main area's add-folder prompt until the first tree read settles", async () => {
  const { hub, answer } = hubWithHeldTree();
  hub.runtimes = async () => [];
  render(<App hub={hub} />);

  expect(await screen.findByText("没有打开的会话")).toBeTruthy();
  expect(screen.queryByText("没有文件夹也能开始：会话会在应用自己的目录里打开")).toBeNull();

  await act(async () => answer([]));

  expect(await screen.findByText("没有文件夹也能开始：会话会在应用自己的目录里打开")).toBeTruthy();
});

// A session with no folder opens in the app's own workspace, so an empty folder
// list still offers a way to start one; adding a folder stays beside it.
it("offers a new session in the main area when there are no folders", async () => {
  const { hub, answer } = hubWithHeldTree();
  hub.runtimes = async () => [];
  const opened: Array<{ root?: string }> = [];
  const open = hub.open.bind(hub);
  hub.open = async (req) => { opened.push(req); return open(req); };
  const { container } = render(<App hub={hub} />);
  await screen.findByText("没有打开的会话");
  await act(async () => answer([]));

  await screen.findByText("没有文件夹也能开始：会话会在应用自己的目录里打开");
  const empty = container.querySelector<HTMLElement>(".panes-empty")!;
  const start = empty.querySelector<HTMLButtonElement>('button[data-action="session.new"]');
  expect(start).toBeTruthy();
  expect(empty.querySelector('button[data-action="workspace.add"]')).toBeTruthy();

  await act(async () => { start!.click(); });
  expect(opened).toEqual([{ root: undefined }]);
});
