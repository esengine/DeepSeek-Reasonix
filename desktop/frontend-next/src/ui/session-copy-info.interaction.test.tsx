// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import type { TreeWorkspace } from "../port/hub";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const SESSION = "/w/.reasonix/sessions/20260901-120000.jsonl";

it("copies the four handoff fields from the session menu", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const tree: TreeWorkspace[] = [
    { root: "/w/project", name: "project", sessions: [{ path: SESSION, name: "20260901-120000", title: "handoff me" }] },
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

  await user.pointer({ keys: "[MouseRight]", target: screen.getByRole("treeitem", { name: /handoff me/ }) });
  await user.click(screen.getByRole("menuitem", { name: "复制会话信息" }));

  expect(write).toHaveBeenCalledWith([
    "会话 ID: 20260901-120000",
    "会话上下文路径: /w/.reasonix/sessions",
    "任务路径: /w/project",
    "任务日志: /w/.reasonix/sessions/20260901-120000.jsonl",
  ].join("\n"));
  expect(screen.queryByRole("menu")).toBeNull();
});
