// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

function draw(reject = false, remembered = true) {
  const tree: TreeWorkspace[] = ["a", "b", "c"].map((name) => ({ root: "/" + name, name, remembered, sessions: [] }));
  const moveWorkspace = reject ? vi.fn().mockRejectedValue(new Error("write failed")) : vi.fn().mockResolvedValue(undefined);
  const reload = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  render(<Workspaces hub={{ moveWorkspace } as never} tree={tree} treeRead runtimes={[]} active=""
    folded={new Set()} onFold={() => {}} reload={reload} onOpen={async () => {}} onFocus={() => {}}
    onClose={async () => {}} liveIds={() => []} runs={{}} onRename={() => {}} onError={onError}
    adder={{ add: () => {}, close: () => {}, at: null } as never} />);
  return { moveWorkspace, reload, onError };
}

it("moves a project through the host and reloads canonical tree order", async () => {
  const { moveWorkspace, reload } = draw();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：b" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "上移" }));
  await waitFor(() => expect(reload).toHaveBeenCalledOnce());
  expect(moveWorkspace).toHaveBeenCalledWith("/b", -1);
  expect(screen.queryByRole("menu")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：b" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "下移" }));
  expect(moveWorkspace).toHaveBeenLastCalledWith("/b", 1);
});

it("disables moves beyond the first and last remembered projects", async () => {
  draw();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：a" }));
  expect((screen.getByRole("menuitem", { name: "上移" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("menuitem", { name: "下移" }) as HTMLButtonElement).disabled).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: "项目操作：c" }));
  expect((screen.getByRole("menuitem", { name: "下移" }) as HTMLButtonElement).disabled).toBe(true);
});

it("reports a host write failure without pretending the order changed", async () => {
  const { reload, onError } = draw(true);
  await userEvent.click(screen.getByRole("button", { name: "项目操作：b" }));
  await userEvent.click(screen.getByRole("menuitem", { name: "下移" }));
  await waitFor(() => expect(onError).toHaveBeenCalledOnce());
  expect(reload).not.toHaveBeenCalled();
});

it("focuses project menu items and supports Arrow/Home/End/Escape", async () => {
  draw();
  const trigger = screen.getByRole("button", { name: "项目操作：b" });
  await userEvent.click(trigger);
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "上移" }));
  await userEvent.keyboard("{ArrowDown}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "下移" }));
  await userEvent.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /从列表移除/ }));
  await userEvent.keyboard("{Home}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "上移" }));
  await userEvent.keyboard("{ArrowUp}");
  expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: /从列表移除/ }));
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(trigger);
});

it("restores project trigger focus after moving", async () => {
  const { reload } = draw();
  const trigger = screen.getByRole("button", { name: "项目操作：b" });
  await userEvent.click(trigger);
  await userEvent.click(screen.getByRole("menuitem", { name: "上移" }));
  await waitFor(() => expect(reload).toHaveBeenCalledOnce());
  expect(document.activeElement).toBe(trigger);
});

it("offers only Remove for an unremembered project", async () => {
  draw(false, false);
  await userEvent.click(screen.getByRole("button", { name: "项目操作：b" }));
  expect(screen.queryByRole("menuitem", { name: "上移" })).toBeNull();
  expect(screen.queryByRole("menuitem", { name: "下移" })).toBeNull();
  expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  expect(screen.getByRole("menuitem", { name: /从列表移除/ })).toBeTruthy();
});
