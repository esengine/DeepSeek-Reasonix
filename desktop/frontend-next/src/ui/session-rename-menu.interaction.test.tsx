// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const SESSION = "/w/.reasonix/sessions/20260901-120000.jsonl";
const tree = (title: string): TreeWorkspace[] => [{ root: "/w", name: "w", sessions: [{ path: SESSION, name: "session", title }] }];

function view(title: string, onRename = vi.fn()) {
  return (
    <Workspaces hub={{} as never} tree={tree(title)} treeRead runtimes={[]} active="" folded={new Set()} onFold={() => {}} reload={async () => {}}
      onOpen={vi.fn().mockResolvedValue(undefined)} onFocus={() => {}} onClose={async () => {}} liveIds={() => []} runs={{}} onRename={onRename}
      onError={() => {}} adder={{ add: () => {}, close: () => {}, at: null } as never} />
  );
}

it("opens the row's menu by right click after an inline rename, before and after the tree catches up", async () => {
  const onRename = vi.fn();
  const { rerender } = render(view("first title", onRename));
  await userEvent.dblClick(screen.getByRole("treeitem", { name: /first title/ }));
  await userEvent.clear(screen.getByRole("textbox", { name: "重命名该会话" }));
  await userEvent.type(screen.getByRole("textbox", { name: "重命名该会话" }), "renamed{Enter}");
  expect(onRename).toHaveBeenCalledWith(SESSION, "renamed", "inline");

  // The kernel has not answered yet: the row still carries the old title.
  fireEvent.contextMenu(screen.getByRole("treeitem", { name: /first title/ }));
  expect(await screen.findByRole("menu")).toBeTruthy();
  fireEvent.keyDown(document.body, { key: "Escape" });

  rerender(view("renamed", onRename));
  fireEvent.contextMenu(screen.getByRole("treeitem", { name: /renamed/ }));
  expect(await screen.findByRole("menu")).toBeTruthy();
});

it.each(["{Shift>}{F10}{/Shift}", "{ContextMenu}"])("opens the local session menu with %s and restores row focus", async (keys) => {
  render(view("keyboard title"));
  const row = screen.getByRole("treeitem", { name: "keyboard title" });
  row.focus();
  await userEvent.keyboard(keys);
  expect(screen.getByRole("menu", { name: "会话操作" })).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "置顶会话" }));
  await userEvent.keyboard("{Escape}");
  expect(document.activeElement).toBe(row);
});
