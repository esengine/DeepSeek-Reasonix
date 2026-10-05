// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const SESSION = "/w/.reasonix/sessions/20260924-120000.jsonl";
const VERSION = "/w/.reasonix/sessions/20260924-130000-version.jsonl";

function draw(onOpen = vi.fn(async (_: { root?: string; sessionPath?: string }) => {})) {
  const tree: TreeWorkspace[] = [
    {
      root: "/w",
      name: "w",
      sessions: [
        { path: SESSION, name: "20260924-120000", title: "the conversation", versions: [{ path: VERSION, name: "20260924-130000-version", turns: 4 }] },
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

// What an edit, regenerate or rewind cut stays under its conversation: folded
// until asked for, named as a version, and opened as a whole conversation.
it("folds an earlier version under its conversation and opens it", async () => {
  const onOpen = draw();
  expect(screen.queryByText("早先版本 · 4 轮")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "+1" }));
  await userEvent.click(screen.getByText("早先版本 · 4 轮"));
  expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ sessionPath: VERSION }));
});
