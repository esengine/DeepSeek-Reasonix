// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { Chrome } from "./Chrome";
import { MockPort } from "../port/mock";
import type { TreeWorkspace } from "../port/hub";

afterEach(cleanup);
const root = "/projects/site";
const copy = "/state/worktrees/one/site";
const path = "/sessions/isolated.jsonl";

function draw() {
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn().mockResolvedValue(undefined);
  const removeWorkspace = vi.fn().mockResolvedValue(undefined);
  const tree = [{ root, name: "site", remembered: true, workspaceRoots: [root, copy], sessions: [
    { path, name: "isolated", title: "隔离中的工作", workspaceRoot: copy, isolated: true },
  ] }] as TreeWorkspace[];
  render(<Workspaces hub={{ removeWorkspace } as never} tree={tree} treeRead
    runtimes={[{ id: "r1", base: "/rt/r1", root: copy, name: "site" }]}
    active="r1" folded={new Set()} onFold={vi.fn()} reload={async () => {}}
    onOpen={onOpen} onFocus={vi.fn()} onClose={onClose} liveIds={() => []}
    runs={{}} onRename={vi.fn()} onError={vi.fn()}
    adder={{ add: vi.fn(), close: vi.fn(), at: null } as never} />);
  return { onOpen, onClose, removeWorkspace };
}

it("opens grouped history in its worktree and new conversations in the original directory", async () => {
  const { onOpen } = draw();
  await userEvent.click(screen.getByText("隔离中的工作"));
  expect(onOpen).toHaveBeenLastCalledWith({ root: copy, sessionPath: path });
  await userEvent.click(screen.getByRole("button", { name: "在 site 下新建会话" }));
  expect(onOpen).toHaveBeenLastCalledWith({ root });
  expect(document.querySelectorAll(".wsnode")).toHaveLength(1);
  expect(document.querySelector(".wsnode[data-current]")).not.toBeNull();
});

it("closes the project's worktree panes when removing the grouped project", async () => {
  const { onClose, removeWorkspace } = draw();
  await userEvent.click(screen.getByRole("button", { name: "项目操作：site" }));
  await userEvent.click(screen.getByRole("menuitem", { name: /从列表移除/ }));
  await userEvent.click(screen.getByRole("button", { name: "移除" }));
  expect(onClose).toHaveBeenCalledWith(["r1"]);
  expect(removeWorkspace).toHaveBeenCalledWith(root);
});

it.each([[false, "原目录"], [true, "隔离目录"]])("identifies the conversation's directory when isolated=%s", async (isolated, label) => {
  const port = new MockPort();
  const current = isolated ? copy : root;
  vi.spyOn(port, "workspaces").mockResolvedValue({ current, isolated, canSwitch: true, canIsolate: true, recents: [] });
  render(<Chrome port={port} status={{ workspaceRoot: current, sessionPath: path } as never}
    title="工作" steer={0} account={null} theme="dark" onTheme={vi.fn()}
    onFind={vi.fn()} view="flow" rows={0} surfaces={0} onView={vi.fn()} />);
  expect(await screen.findByText(label)).toBeTruthy();
  expect(screen.getByText(label).getAttribute("title")).toBe(current);
});

it("ignores old checkout metadata when switching conversations before it arrives", async () => {
  const old = new MockPort();
  const next = new MockPort();
  let finish!: (value: Awaited<ReturnType<MockPort["workspaces"]>>) => void;
  vi.spyOn(old, "workspaces").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  vi.spyOn(next, "workspaces").mockResolvedValue({ current: root, isolated: false, canSwitch: true, canIsolate: true, recents: [] });
  const props = { title: "工作", steer: 0, account: null, theme: "dark" as const,
    onTheme: vi.fn(), onFind: vi.fn(), view: "flow" as const, rows: 0, surfaces: 0, onView: vi.fn() };
  const { rerender } = render(<Chrome {...props} port={old} status={{ workspaceRoot: copy } as never} />);
  rerender(<Chrome {...props} port={next} status={{ workspaceRoot: root } as never} />);
  expect(await screen.findByText("原目录")).toBeTruthy();
  await act(async () => finish({ current: copy, isolated: true, canSwitch: true, canIsolate: true, recents: [] }));
  expect(screen.getByText("原目录").hidden).toBe(false);
  expect(screen.queryByText("隔离目录")).toBeNull();
});
