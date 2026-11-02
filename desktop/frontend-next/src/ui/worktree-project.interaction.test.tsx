// @vitest-environment jsdom
import type { ComponentProps } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { Chrome as ChromeView } from "./Chrome";
import { MockPort } from "../port/mock";
import type { TreeWorkspace } from "../port/hub";
import type { WorkspaceInfo } from "../port/port";
import { useWorkspaceInfo } from "./useWorkspaceInfo";

function Chrome(props: Omit<ComponentProps<typeof ChromeView>, "workspace">) {
  const workspace = useWorkspaceInfo(props.port, props.status?.workspaceRoot || props.status?.cwd || "", props.status !== null);
  return <ChromeView {...props} workspace={workspace} />;
}

afterEach(cleanup);

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

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

it.each([false, true])("only shows a directory badge when isolated=%s", async (isolated) => {
  const port = new MockPort();
  const current = isolated ? copy : root;
  vi.spyOn(port, "workspaces").mockResolvedValue({ current, isolated, canSwitch: true, canIsolate: true, recents: [] });
  await act(async () => { render(<Chrome port={port} status={{ workspaceRoot: current, sessionPath: path } as never}
    title="工作" steer={0} account={null} theme="dark" onTheme={vi.fn()}
    onFind={vi.fn()} view="flow" rows={0} surfaces={0} onView={vi.fn()} />); });
  expect(screen.queryByText("项目目录")).toBeNull();
  if (isolated) {
    expect(screen.getByText("独立工作树").getAttribute("title")).toBe(`在单独的 Git 工作目录中修改代码；端口、数据库等运行资源仍可能共享。\n${current}`);
  } else {
    expect(screen.queryByText("独立工作树")).toBeNull();
  }
});

it.each([root, copy])("ignores old checkout metadata when switching ports to %s", async (current) => {
  const old = new MockPort();
  const next = new MockPort();
  const { promise, resolve } = deferred<WorkspaceInfo>();
  vi.spyOn(old, "workspaces").mockReturnValue(promise);
  const reads = vi.spyOn(next, "workspaces").mockResolvedValue({ current, isolated: false, canSwitch: true, canIsolate: true, recents: [] });
  const props = { title: "工作", steer: 0, account: null, theme: "dark" as const,
    onTheme: vi.fn(), onFind: vi.fn(), view: "flow" as const, rows: 0, surfaces: 0, onView: vi.fn() };
  const { rerender } = render(<Chrome {...props} port={old} status={{ workspaceRoot: copy } as never} />);
  await act(async () => { rerender(<Chrome {...props} port={next} status={{ workspaceRoot: current } as never} />); });
  await act(async () => resolve({ current: copy, isolated: true, canSwitch: true, canIsolate: true, recents: [] }));
  expect(screen.queryByText("项目目录")).toBeNull();
  expect(screen.queryByText("独立工作树")).toBeNull();
  expect(reads).toHaveBeenCalledTimes(1);
});

it("reads again for a changed root on the same port and rejects its stale response", async () => {
  const port = new MockPort();
  const { promise, resolve } = deferred<WorkspaceInfo>();
  const reads = vi.spyOn(port, "workspaces")
    .mockReturnValueOnce(promise)
    .mockResolvedValue({ current: root, isolated: true, canSwitch: true, canIsolate: true, recents: [] });
  const props = { port, title: "工作", steer: 0, account: null, theme: "dark" as const,
    onTheme: vi.fn(), onFind: vi.fn(), view: "flow" as const, rows: 0, surfaces: 0, onView: vi.fn() };
  const { rerender } = render(<Chrome {...props} status={{ workspaceRoot: copy } as never} />);
  rerender(<Chrome {...props} status={{ workspaceRoot: root } as never} />);
  await waitFor(() => expect(screen.getByText("独立工作树").title).toContain(root));
  await act(async () => resolve({ current: copy, isolated: true, canSwitch: true, canIsolate: true, recents: [] }));
  expect(screen.getByText("独立工作树").title).toContain(root);
  expect(reads).toHaveBeenCalledTimes(2);
  rerender(<Chrome {...props} status={{ workspaceRoot: root, running: true, sessionPath: path } as never} />);
  expect(reads).toHaveBeenCalledTimes(2);
});

it("waits for a directory, accepts cwd, and clears the badge when the pane closes", async () => {
  const port = new MockPort();
  const reads = vi.spyOn(port, "workspaces").mockResolvedValue({ current: root, isolated: true, canSwitch: true, canIsolate: true, recents: [] });
  const props = { title: "工作", steer: 0, account: null, theme: "dark" as const,
    onTheme: vi.fn(), onFind: vi.fn(), view: "flow" as const, rows: 0, surfaces: 0, onView: vi.fn() };
  const { rerender } = render(<Chrome {...props} port={null} status={null} />);
  rerender(<Chrome {...props} port={port} status={{} as never} />);
  expect(reads).not.toHaveBeenCalled();
  rerender(<Chrome {...props} port={port} status={{ cwd: root } as never} />);
  await screen.findByText("独立工作树");
  expect(reads).toHaveBeenCalledTimes(1);
  rerender(<Chrome {...props} port={null} status={null} />);
  expect(screen.queryByText("独立工作树")).toBeNull();
});
