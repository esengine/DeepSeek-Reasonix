// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";

const shell = vi.hoisted(() => ({ reveals: true }));
vi.mock("../port/host", () => ({ host: () => ({ revealsFiles: () => shell.reveals }) }));
import type { OpenWithTarget, TreeWorkspace } from "../port/hub";

afterEach(cleanup);

function draw(targets: OpenWithTarget[], over: Partial<TreeWorkspace> = {}, reveals = true) {
  const tree: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [], ...over }];
  const openWith = vi.fn().mockResolvedValue(undefined);
  const openWithTargets = vi.fn().mockResolvedValue(targets);
  const revealWorkspace = vi.fn().mockResolvedValue(undefined);
  const onError = vi.fn();
  shell.reveals = reveals;
  render(
    <Workspaces
      hub={{ revealWorkspace, openWith, openWithTargets } as never}
      tree={tree} treeRead runtimes={[]} active="" folded={new Set()} onFold={() => {}} reload={async () => {}}
      onOpen={async () => {}} onFocus={() => {}} onClose={async () => {}} liveIds={() => []} runs={{}}
      onRename={() => {}} onError={onError} adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
  return { openWith, openWithTargets, onError };
}

const openMenu = () => userEvent.click(screen.getByRole("button", { name: /项目操作：/ }));
const TARGETS: OpenWithTarget[] = [
  { id: "vscode", name: "Visual Studio Code", available: true },
  { id: "cursor", name: "Cursor", available: false },
  { id: "terminal", name: "Terminal", available: true },
];

it("offers only the programs this machine has, and opens the project in the chosen one", async () => {
  const { openWith } = draw(TARGETS);
  await openMenu();
  expect(await screen.findByRole("menuitem", { name: /用 Visual Studio Code 打开/ })).toBeTruthy();
  expect(screen.getByRole("menuitem", { name: /用 终端 打开/ })).toBeTruthy();
  expect(screen.queryByRole("menuitem", { name: /Cursor/ })).toBeNull();

  await userEvent.click(screen.getByRole("menuitem", { name: /用 Visual Studio Code 打开/ }));
  expect(openWith).toHaveBeenCalledWith("/w", "vscode");
});

it("shows no entries for a project whose folder is gone, or where no program is installed", async () => {
  const gone = draw(TARGETS, { missing: true });
  await openMenu();
  await waitFor(() => expect(gone.openWithTargets).toHaveBeenCalled());
  expect(screen.getByRole("menuitem", { name: /用 Visual Studio Code 打开/ }).hasAttribute("disabled")).toBe(true);
  cleanup();
  draw([{ id: "vscode", name: "Visual Studio Code", available: false }]);
  await openMenu();
  await waitFor(() => expect(screen.queryByRole("menuitem", { name: /用 .* 打开/ })).toBeNull());
});

it("reports a failed launch where the person is looking", async () => {
  const { openWith, onError } = draw(TARGETS);
  const refusal = new Error("not installed");
  openWith.mockRejectedValue(refusal);
  await openMenu();
  await userEvent.click(await screen.findByRole("menuitem", { name: /用 终端 打开/ }));
  await waitFor(() => expect(onError).toHaveBeenCalledWith(refusal));
});

it("is not offered by a window that cannot start programs on this machine", async () => {
  const { openWithTargets } = draw(TARGETS, {}, false);
  await openMenu();
  expect(screen.queryByRole("menuitem", { name: /用 .* 打开/ })).toBeNull();
  expect(openWithTargets).not.toHaveBeenCalled();
});

it("names the project folder that keeps an installed program off the list", async () => {
  const { openWith } = draw([
    { id: "vscode", name: "Visual Studio Code", available: true },
    { id: "cursor", name: "Cursor", available: false, why: "inside_project", within: "C:\\Users\\u" },
  ]);
  await openMenu();
  const blocked = await screen.findByRole("menuitem", { name: /用 Cursor 打开/ });
  expect(blocked.hasAttribute("disabled")).toBe(true);
  expect(blocked.getAttribute("title")).toContain("C:\\Users\\u");
  await userEvent.click(blocked);
  expect(openWith).not.toHaveBeenCalled();
});
