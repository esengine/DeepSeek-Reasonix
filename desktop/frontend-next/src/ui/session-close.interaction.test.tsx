// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);

const SESSION = "/w/.reasonix/sessions/20260901-120000.jsonl";

// A single open pane has no tab strip, so the row's menu is the one place a
// conversation can be closed from.
function draw(live: boolean, held: string | null = "r1") {
  const runtimeId = held ?? undefined;
  const workspaces: TreeWorkspace[] = [
    { root: "/w", name: "w", sessions: [{ path: SESSION, name: "session", title: "open here", runtimeId }] },
  ];
  const onClose = vi.fn().mockResolvedValue(undefined);
  render(
    <Workspaces
      hub={{} as never}
      tree={workspaces}
      treeRead
      runtimes={runtimeId ? [{ id: runtimeId, base: "", root: "/w", name: "w", sessionPath: SESSION }] : []}
      active=""
      folded={new Set()}
      onFold={() => {}}
      reload={async () => {}}
      onOpen={async () => {}}
      onFocus={() => {}}
      onClose={onClose}
      liveIds={(ids) => (live ? ids : [])}
      runs={runtimeId ? { [runtimeId]: { run: live ? "running" : "idle", live } } : {}}
      onRename={() => {}}
      onError={() => {}}
      adder={{ add: () => {}, close: () => {}, at: null } as never}
    />,
  );
  return { onClose };
}

it("closes an idle open conversation from its row", async () => {
  const { onClose } = draw(false);
  await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /open here/ }) });
  await userEvent.click(screen.getByRole("menuitem", { name: "关闭会话" }));
  expect(onClose).toHaveBeenCalledWith(["r1"]);
});

it("will not close a conversation mid-turn", async () => {
  const { onClose } = draw(true);
  await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /open here/ }) });
  const close = screen.getByRole("menuitem", { name: "关闭会话" }) as HTMLButtonElement;
  expect(close.disabled).toBe(true);
  expect(onClose).not.toHaveBeenCalled();
});

it("offers no close for a conversation no pane holds", async () => {
  draw(false, null);
  await userEvent.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /open here/ }) });
  expect(screen.queryByRole("menuitem", { name: "关闭会话" })).toBeNull();
});
