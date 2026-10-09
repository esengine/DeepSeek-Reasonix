// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { TreeWorkspace } from "../port/hub";
import { boot, STORAGE } from "../i18n";

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  sessionStorage.clear();
  localStorage.clear();
  vi.restoreAllMocks();
});

function readyHub() {
  const hub = new MockHub();
  hub.remoteHosts = async () => [];
  const portFor = hub.portFor.bind(hub);
  hub.portFor = (runtime) => {
    const port = portFor(runtime);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return hub;
}

async function refreshFromNewSession(hub: MockHub) {
  let resolve!: (tree: TreeWorkspace[]) => void;
  let reject!: (error: Error) => void;
  const pending = new Promise<TreeWorkspace[]>((yes, no) => { resolve = yes; reject = no; });
  const read = vi.spyOn(hub, "tree").mockReturnValue(pending);
  await userEvent.keyboard("{Control>}k{/Control}");
  await userEvent.click(await screen.findByRole("button", { name: /^新建会话操作$/ }));
  await waitFor(() => expect(read).toHaveBeenCalled());
  return { resolve, reject };
}

it("keeps the listed conversations when a refresh after opening a session fails", async () => {
  const hub = readyHub();
  render(<App hub={hub} />);
  await screen.findByText("上一次的会话");
  expect(screen.getByText("my-website")).toBeTruthy();
  await screen.findByRole("combobox", { name: "任务输入" });
  const pending = await refreshFromNewSession(hub);
  await act(async () => pending.reject(new Error("temporary tree read failure")));
  expect(screen.queryByText("上一次的会话")).toBeTruthy();
  expect(screen.queryByText("my-website")).toBeTruthy();
  expect(screen.queryByText("尚无文件夹")).toBeNull();
});

it("accepts a successful empty refresh as the new canonical tree", async () => {
  const hub = readyHub();
  render(<App hub={hub} />);
  await screen.findByText("上一次的会话");
  await screen.findByRole("combobox", { name: "任务输入" });
  const pending = await refreshFromNewSession(hub);
  await act(async () => pending.resolve([]));
  expect(screen.queryByText("上一次的会话")).toBeNull();
  expect(screen.queryByText("my-website")).toBeNull();
  expect(screen.getAllByText("尚无文件夹").length).toBeGreaterThan(0);
});
