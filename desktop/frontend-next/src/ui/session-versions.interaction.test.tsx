// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const SESSION = "/w/.reasonix/sessions/20260924-120000.jsonl";
const VERSION = "/w/.reasonix/sessions/20260924-130000-version.jsonl";
const VERSION_TITLE = "the conversation before the last edit";

function draw(onOpen = vi.fn(async (_: { root?: string; sessionPath?: string }) => {})) {
  const tree: TreeWorkspace[] = [
    {
      root: "/w",
      name: "w",
      sessions: [
        { path: SESSION, name: "20260924-120000", title: "the conversation", versions: [{ path: VERSION, name: "20260924-130000-version", title: VERSION_TITLE, turns: 4 }] },
      ],
    } as TreeWorkspace,
  ];
  render(
    <Workspaces
      hub={{} as never}
      tree={tree}
      treeRead
      runtimes={[]}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={onOpen}
      onFocus={() => {}}
      onClose={async () => {}}
      liveIds={() => []}
      runs={{}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
  return onOpen;
}

const openVersions = async (row: HTMLElement) => {
  fireEvent.contextMenu(row);
  await userEvent.click(await screen.findByRole("menuitem", { name: /早先版本（\d+）/ }));
};

// What an edit, regenerate or rewind cut stays under its conversation: folded
// until asked for, named as a version, and opened as a whole conversation.
it("folds an earlier version under its conversation and opens it", async () => {
  const onOpen = draw();
  expect(screen.queryByText("早先版本 · 4 轮")).toBeNull();
  await openVersions(screen.getAllByRole("treeitem").find((e) => e.getAttribute("aria-level") === "2")!);
  expect(screen.getByTitle(VERSION_TITLE).textContent).toBe("早先版本 · 4 轮");
  await userEvent.click(screen.getByText("早先版本 · 4 轮"));
  expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ sessionPath: VERSION }));
});
