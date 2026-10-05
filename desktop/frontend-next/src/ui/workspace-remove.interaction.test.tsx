// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

// Removing a project is a menu item behind a confirm, never a glyph beside
// "new session" that reads as "close".
function draw() {
  const workspaces: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [] }];
  const removeWorkspace = vi.fn().mockResolvedValue(undefined);
  render(
    <Workspaces
      hub={{ removeWorkspace } as never}
      tree={workspaces}
      treeRead
      runtimes={[]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={async () => {}}
      onFocus={() => {}}
      onClose={async () => {}}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
  return { removeWorkspace };
}

it("puts nothing destructive on the project row itself", () => {
  draw();
  expect(screen.queryByRole("button", { name: "从列表移除" })).toBeNull();
});

it("removes a project from its menu after a confirm", async () => {
  const { removeWorkspace } = draw();
  await userEvent.click(screen.getByRole("button", { name: /项目操作：/ }));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  expect(removeWorkspace).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "移除" }));
  expect(removeWorkspace).toHaveBeenCalledWith("/w");
});
