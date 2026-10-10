// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";

const shell = vi.hoisted(() => ({ reveals: true }));
vi.mock("../port/host", () => ({ host: () => ({ revealsFiles: () => shell.reveals }) }));
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

function draw(over: Partial<TreeWorkspace> = {}, reveals = true) {
  const tree: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [], ...over }];
  const revealWorkspace = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  shell.reveals = reveals;
  render(
    <Workspaces
      hub={{ revealWorkspace } as never}
      tree={tree}
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
      onError={onError}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
  return { revealWorkspace, onError };
}

const openMenu = () => userEvent.click(screen.getByRole("button", { name: /项目操作：/ }));

it("shows a project's folder in the file manager from its menu", async () => {
  const { revealWorkspace } = draw();
  await openMenu();
  await userEvent.click(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  expect(revealWorkspace).toHaveBeenCalledWith("/w");
});

it("says why it cannot when the folder is gone", async () => {
  const { revealWorkspace } = draw({ missing: true });
  await openMenu();
  const item = screen.getByRole("menuitem", { name: /在文件管理器中显示/ });
  expect((item as HTMLButtonElement).disabled).toBe(true);
  expect(item.textContent).toContain("文件夹已不在磁盘上");
  expect(revealWorkspace).not.toHaveBeenCalled();
});

it("offers nothing where the window cannot show files", async () => {
  draw({}, false);
  await openMenu();
  expect(screen.queryByRole("menuitem", { name: /在文件管理器中显示/ })).toBeNull();
});

it("reports a refusal instead of swallowing it", async () => {
  const { revealWorkspace, onError } = draw();
  const refusal = new Error("not listed");
  revealWorkspace.mockRejectedValueOnce(refusal);
  await openMenu();
  await userEvent.click(screen.getByRole("menuitem", { name: /在文件管理器中显示/ }));
  await vi.waitFor(() => expect(onError).toHaveBeenCalledWith(refusal));
});
