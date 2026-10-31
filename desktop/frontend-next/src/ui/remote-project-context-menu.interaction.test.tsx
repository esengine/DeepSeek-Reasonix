// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RemoteHosts } from "./RemoteHosts";

afterEach(cleanup);

function draw() {
  const readTree = vi.fn().mockResolvedValue(undefined);
  const onFold = vi.fn();
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const removeRemoteWorkspace = vi.fn().mockResolvedValue(undefined);
  render(<RemoteHosts hub={{ removeRemoteWorkspace } as never}
    hosts={[{ name: "gpu", target: "gpu", status: "idle" }]}
    trees={{ gpu: ["a", "b"].map(name => ({ root: "/" + name, name, sessions: [{ path: "/s/" + name, name: "Task " + name }] })) }}
    runtimes={[]} active="" onOpen={onOpen} onFocus={vi.fn()} onClose={async () => {}} liveIds={() => []}
    reload={async () => {}} reloadTrees={async () => {}} readTree={readTree} onFold={onFold} onError={vi.fn()} />);
  const row = (name: string) => screen.getByText(name, { selector: ".wsname" }).closest<HTMLElement>('[role="treeitem"]')!;
  return { row, readTree, onFold, onOpen, removeRemoteWorkspace };
}

it("opens the remote menu at the pointer without folding or starting a session", async () => {
  const { row, onFold, onOpen, readTree } = draw();
  expect(fireEvent.contextMenu(row("b"), { clientX: 120, clientY: 90 })).toBe(false);
  const menu = screen.getByRole("menu");
  expect(menu.style.left).toBe("120px");
  expect(menu.style.top).toBe("90px");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "刷新 gpu 上的会话" }));
  expect(onFold).not.toHaveBeenCalled();
  expect(onOpen).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("menuitem", { name: "刷新 gpu 上的会话" }));
  expect(readTree).toHaveBeenCalledExactlyOnceWith("gpu");
  expect(document.activeElement).toBe(row("b"));
});

it.each(["{ContextMenu}", "{Shift>}{F10}{/Shift}"])("opens from %s and restores row focus on Escape", async key => {
  const { row } = draw();
  row("b").focus();
  await userEvent.keyboard(key);
  expect(screen.getByRole("menu")).toBeTruthy();
  await userEvent.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /从列表移除/ }));
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(row("b"));
});

it("switches rows through one menu and retains the more-button and normal click behavior", async () => {
  const { row, removeRemoteWorkspace, onFold, onOpen } = draw();
  fireEvent.contextMenu(row("a"));
  fireEvent.contextMenu(row("b"));
  expect(screen.getAllByRole("menu")).toHaveLength(1);
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(removeRemoteWorkspace).toHaveBeenCalledExactlyOnceWith("gpu", "/b", ["/b"]);
  const more = screen.getByRole("button", { name: "项目操作：a · gpu" });
  await userEvent.click(more);
  await userEvent.keyboard("{Escape}");
  expect(document.activeElement).toBe(more);
  await userEvent.click(row("a"));
  expect(onFold).toHaveBeenCalledExactlyOnceWith('["gpu","/a"]', true);
  await userEvent.click(screen.getByRole("button", { name: "在 a · gpu 下新建会话" }));
  expect(onOpen).toHaveBeenCalledExactlyOnceWith("gpu", "/a", undefined);
});
