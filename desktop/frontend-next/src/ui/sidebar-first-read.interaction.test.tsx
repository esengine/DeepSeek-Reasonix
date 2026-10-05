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
  expect(screen.queryByText("会话在文件夹里打开，先添加一个")).toBeNull();

  await act(async () => answer([]));

  expect(await screen.findByText("会话在文件夹里打开，先添加一个")).toBeTruthy();
});
