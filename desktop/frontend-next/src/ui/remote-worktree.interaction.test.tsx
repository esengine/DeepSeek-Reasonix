// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RemoteHosts } from "./RemoteHosts";
import { Palette } from "./Palette";
import type { TreeWorkspace } from "../port/hub";
import type { RemoteHost } from "../port/remote";

afterEach(cleanup);
const root = "/projects/site";
const copy = "/state/worktrees/site";
const host = { name: "gpu", target: "test@host", status: "connected" as const, workspace: root, workspaces: [root, copy] };

it.each([undefined, "gpu"])("quick switching retains a grouped session's actual directory on %s", async (source) => {
  const onOpenSession = vi.fn();
  render(<Palette open commands={[]} onClose={vi.fn()} onOpenSession={onOpenSession}
    tree={[{ root, name: "site", host: source, sessions: [
      { path: "/sessions/task", name: "Isolated task", workspaceRoot: copy },
    ] }]} />);
  await userEvent.click(screen.getByText("Isolated task"));
  expect(onOpenSession).toHaveBeenCalledWith("/sessions/task", expect.objectContaining({ root: copy, host: source }));
});

function draw(open = false, source: RemoteHost = host, removeRemoteWorkspace = vi.fn().mockResolvedValue(undefined)) {
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  const reload = vi.fn().mockResolvedValue(undefined);
  const tree: TreeWorkspace[] = [{ root, name: "site", workspaceRoots: [root, copy], sessions: [
    { path: "/sessions/task", name: "Isolated task", workspaceRoot: copy, isolated: true },
  ] }];
  render(<RemoteHosts hub={{ removeRemoteWorkspace } as never} hosts={[source]} trees={{ gpu: tree }}
    runtimes={open ? [{ id: "r1", host: "gpu", base: "", root: copy, name: "site", sessionPath: "/sessions/task" }] : []}
    active={open ? "r1" : ""} onOpen={onOpen} onFocus={vi.fn()} onClose={onClose} liveIds={() => []}
    reload={reload} reloadTrees={async () => {}} readTree={async () => {}} onError={onError} />);
  return { onOpen, onClose, removeRemoteWorkspace, onError, reload };
}

it("keeps SSH worktree history in its source project and opens its actual directory", async () => {
  const { onOpen } = draw();
  expect(document.querySelectorAll(".wsnode")).toHaveLength(1);
  expect(screen.getByText("隔离").getAttribute("title")).toBe(copy);
  await userEvent.click(screen.getByText("Isolated task"));
  expect(onOpen).toHaveBeenLastCalledWith("gpu", copy, "/sessions/task");
  await userEvent.click(screen.getByRole("button", { name: "在 site · gpu 下新建会话" }));
  expect(onOpen).toHaveBeenLastCalledWith("gpu", root, undefined);
});

it("marks the grouped SSH project current and removes all its booked directories", async () => {
  const { onClose, removeRemoteWorkspace } = draw(true);
  expect(document.querySelectorAll(".wsnode")).toHaveLength(1);
  expect(document.querySelector(".wsnode[data-current]")).not.toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(onClose).toHaveBeenCalledWith(["r1"]);
  expect(removeRemoteWorkspace.mock.calls).toEqual([["gpu", root, [root, copy]]]);
  expect(removeRemoteWorkspace.mock.invocationCallOrder[0]).toBeLessThan(onClose.mock.invocationCallOrder[0]);
});

it("keeps every pane and project when the kernel refuses removal", async () => {
  const error = new Error("workspace.running");
  const { onClose, onError, reload } = draw(true, host, vi.fn().mockRejectedValue(error));
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(onClose).not.toHaveBeenCalled();
  expect(reload).not.toHaveBeenCalled();
  expect(onError).toHaveBeenCalledWith(error);
  expect(screen.getByText("Isolated task")).toBeTruthy();
});

it("shows the active SSH directory separately from the host's default read target", async () => {
  draw(true);
  expect(screen.getByText("连接目录：" + copy)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  expect(screen.getByText("默认连接目录：" + root)).toBeTruthy();
});

it("removes an unbooked remote project while offline", async () => {
  const { removeRemoteWorkspace } = draw(false, { ...host, status: "idle", workspace: undefined, workspaces: [] });
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(removeRemoteWorkspace).toHaveBeenCalledWith("gpu", root, [root, copy]);
});

it("uses the server's resolved default even when it differs from the project list's order", async () => {
  draw(false, { ...host, workspace: copy });
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  expect(screen.getByText("默认连接目录：" + copy)).toBeTruthy();
});

it("does not invent a default from the project list when the server reports none", async () => {
  draw(false, { ...host, workspace: undefined });
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site · gpu" }));
  expect(screen.getByText("未设置默认工作区")).toBeTruthy();
});
