// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

vi.mock("../port/host", () => ({ host: () => ({ revealsFiles: () => true }) }));

afterEach(cleanup);

function draw(over: Partial<TreeWorkspace> = {}) {
  const tree: TreeWorkspace[] = ["a", "b"].map((name) => ({
    root: "/" + name, name, remembered: true, sessions: [], ...over,
  }));
  const revealWorkspace = vi.fn().mockResolvedValue(undefined);
  const moveWorkspace = vi.fn().mockResolvedValue(undefined);
  const removeWorkspace = vi.fn().mockResolvedValue(undefined);
  const reload = vi.fn().mockResolvedValue(undefined);
  const onFold = vi.fn();
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  render(<Workspaces hub={{ revealWorkspace, moveWorkspace, removeWorkspace } as never} tree={tree} treeRead
    runtimes={[]} active="" folded={new Set()} onFold={onFold} reload={reload} onOpen={onOpen}
    onFocus={() => {}} onClose={async () => {}} liveIds={() => []} runs={{}} onRename={() => {}}
    onError={onError} adder={{ add: () => {}, close: () => {}, at: null } as never} />);
  const row = (name: string) => screen.getByText(name, { selector: ".wsname" }).closest<HTMLElement>('[role="treeitem"]')!;
  return { row, revealWorkspace, moveWorkspace, removeWorkspace, reload, onFold, onOpen, onError };
}

it("opens the existing project menu from a right click without folding or opening a session", () => {
  const { row, onFold, onOpen } = draw();
  expect(fireEvent.contextMenu(row("b"), { clientX: 120, clientY: 90 })).toBe(false);
  expect(screen.getByRole("menu", { name: "项目操作" })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: /在文件管理器中显示/ })).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  expect(onFold).not.toHaveBeenCalled();
  expect(onOpen).not.toHaveBeenCalled();
});

it.each(["{ContextMenu}", "{Shift>}{F10}{/Shift}"])("opens from %s and restores row focus on Escape", async (key) => {
  const { row } = draw();
  row("b").focus();
  await userEvent.keyboard(key);
  expect(screen.getByRole("menu", { name: "项目操作" })).toBeTruthy();
  await userEvent.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /从列表移除/ }));
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(row("b"));
});

it("reveals the right-clicked project and returns focus to its row", async () => {
  const { row, revealWorkspace } = draw();
  fireEvent.contextMenu(row("b"));
  await userEvent.click(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  expect(revealWorkspace).toHaveBeenCalledExactlyOnceWith("/b");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(row("b"));
});

it("reuses project movement and removal confirmation from the context menu", async () => {
  const { row, moveWorkspace, removeWorkspace, reload } = draw();
  fireEvent.contextMenu(row("b"));
  await userEvent.click(screen.getByRole("menuitem", { name: "上移" }));
  await waitFor(() => expect(reload).toHaveBeenCalledOnce());
  expect(moveWorkspace).toHaveBeenCalledExactlyOnceWith("/b", -1);
  fireEvent.contextMenu(row("b"));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(screen.getByRole("alertdialog").textContent).toContain("从列表移除「b」？");
  expect(removeWorkspace).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "移除" }));
  await waitFor(() => expect(removeWorkspace).toHaveBeenCalledExactlyOnceWith("/b"));
});

it("switches the context menu to another project and preserves normal row and add clicks", async () => {
  const { row, revealWorkspace, onFold, onOpen } = draw();
  fireEvent.contextMenu(row("a"));
  fireEvent.contextMenu(row("b"));
  expect(screen.getAllByRole("menu")).toHaveLength(1);
  await userEvent.click(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  expect(revealWorkspace).toHaveBeenCalledExactlyOnceWith("/b");
  await userEvent.click(row("a"));
  expect(onFold).toHaveBeenCalledExactlyOnceWith("/a", true);
  await userEvent.click(screen.getByRole("button", { name: "在 b 下新建会话" }));
  expect(onOpen).toHaveBeenCalledExactlyOnceWith({ root: "/b" });
  expect(screen.queryByRole("menu")).toBeNull();
});

it("keeps unavailable actions disabled and reports reveal failures", async () => {
  const missing = draw({ missing: true });
  fireEvent.contextMenu(missing.row("b"));
  expect((screen.getByRole("menuitem", { name: /在文件管理器中显示/ }) as HTMLButtonElement).disabled).toBe(true);
  expect(missing.revealWorkspace).not.toHaveBeenCalled();
  cleanup();
  const { row, revealWorkspace, onError } = draw();
  const refusal = new Error("folder unavailable");
  revealWorkspace.mockRejectedValueOnce(refusal);
  fireEvent.contextMenu(row("b"));
  await userEvent.click(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  await waitFor(() => expect(onError).toHaveBeenCalledExactlyOnceWith(refusal));
});
