// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Workspaces } from "./Workspaces";
import { RailSearch } from "./railsearch";
import { draftKey, readDraft, writeDraft } from "./drafts";
import type { TreeSession, TreeWorkspace } from "../port/hub";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const sessions: TreeSession[] = [
  { path: "/sessions/a.jsonl", name: "a", title: "Alpha", runtimeId: "r1" },
  { path: "/sessions/b.jsonl", name: "b", title: "Beta" },
  { path: "/sessions/c.jsonl", name: "c", title: "Gamma", archived: true },
];

function draw(rows = sessions, scope: "all" | "archived" = "all", folded = new Set<string>()) {
  const hub = { removeSession: vi.fn().mockResolvedValue(undefined) };
  const onArchive = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn().mockResolvedValue(undefined);
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onFocus = vi.fn();
  const onError = vi.fn();
  const reload = vi.fn().mockResolvedValue(undefined);
  let live: string[] = [];
  const props = { hub: hub as never, tree: [{ root: "/w", name: "Project", sessions: rows }] as TreeWorkspace[],
    treeRead: true, runtimes: [{ id: "r1" }] as never, active: "r1", folded, onFold: vi.fn(), reload,
    onOpen, onFocus, onClose, liveIds: (ids: string[]) => ids.filter((id) => live.includes(id)), runs: {},
    scope, onArchive, onRename: vi.fn(), onError, adder: { add: vi.fn(), close: vi.fn(), at: null } as never };
  const view = render(<RailSearch><Workspaces {...props} /></RailSearch>);
  return { hub, onArchive, onClose, onOpen, onFocus, onError, reload,
    live: (ids: string[]) => { live = ids; view.rerender(<RailSearch><Workspaces {...props}
      runs={Object.fromEntries(ids.map((id) => [id, { run: "running", live: true }]))} /></RailSearch>); },
    update: (tree: TreeWorkspace[]) => { props.tree = tree; view.rerender(<RailSearch><Workspaces {...props} /></RailSearch>); },
    fold: (roots: Set<string>) => { props.folded = roots; view.rerender(<RailSearch><Workspaces {...props} /></RailSearch>); } };
}

async function selectAll() {
  await userEvent.click(screen.getByRole("button", { name: "选择本机会话" }));
  await userEvent.click(screen.getByRole("button", { name: "全选当前显示的会话" }));
}

it("selects rows with clicks and the keyboard without opening or focusing a pane", async () => {
  const pane = draw();
  await userEvent.click(screen.getByRole("button", { name: "选择本机会话" }));
  const alpha = screen.getByRole("treeitem", { name: /Alpha/ });
  await userEvent.click(alpha);
  const beta = screen.getByRole("treeitem", { name: /Beta/ });
  beta.focus();
  await userEvent.keyboard(" ");
  expect(alpha.getAttribute("aria-selected")).toBe("true");
  expect(beta.getAttribute("aria-selected")).toBe("true");
  expect(screen.getByText("已选 2 个会话")).toBeTruthy();
  expect(pane.onOpen).not.toHaveBeenCalled();
  expect(pane.onFocus).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "完成选择" }));
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "选择本机会话" }));
  await userEvent.click(beta);
  expect(pane.onOpen).toHaveBeenCalledWith({ root: "/w", sessionPath: "/sessions/b.jsonl" });
});

it("archives selected sessions through the same owner as an individual row", async () => {
  const pane = draw();
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "归档所选会话" }));
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledTimes(2));
  expect(pane.onArchive).toHaveBeenNthCalledWith(1, "/sessions/a.jsonl", true, "r1");
  expect(pane.onArchive).toHaveBeenNthCalledWith(2, "/sessions/b.jsonl", true, undefined);
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
});

it("restores only the selected archived rows", async () => {
  const pane = draw(sessions, "archived");
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "取消所选会话归档" }));
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledWith("/sessions/c.jsonl", false, undefined));
  expect(pane.onArchive).toHaveBeenCalledTimes(1);
});

it("confirms the selected names before deletion and lets Escape cancel without a mutation", async () => {
  const pane = draw();
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "删除所选会话" }));
  const question = screen.getByRole("alertdialog", { name: "删除 2 个会话？" });
  expect(within(question).getByText("Alpha、Beta")).toBeTruthy();
  expect(pane.hub.removeSession).not.toHaveBeenCalled();
  await userEvent.keyboard("{Escape}");
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "删除所选会话" }));
  expect(pane.onClose).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "删除所选会话" }));
  await userEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(pane.hub.removeSession).toHaveBeenCalledTimes(2));
  expect(pane.onClose).toHaveBeenCalledWith(["r1"]);
  expect(pane.onClose.mock.invocationCallOrder[0]).toBeLessThan(pane.hub.removeSession.mock.invocationCallOrder[0]!);
  expect(pane.hub.removeSession).toHaveBeenNthCalledWith(1, "/sessions/a.jsonl");
  expect(pane.hub.removeSession).toHaveBeenNthCalledWith(2, "/sessions/b.jsonl");
  expect(pane.reload).toHaveBeenCalledTimes(1);
});

it("keeps failed rows selected while removing successful rows from the batch", async () => {
  const pane = draw();
  const failure = new Error("storage unavailable");
  pane.onArchive.mockRejectedValueOnce(failure);
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "归档所选会话" }));
  await waitFor(() => expect(pane.onError).toHaveBeenCalledWith(failure));
  expect(screen.getByText("已选 1 个会话")).toBeTruthy();
  expect(screen.getByRole("treeitem", { name: /Alpha/ }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("treeitem", { name: /Beta/ }).getAttribute("aria-selected")).toBe("false");
});

it("does not stop running sessions, including a turn that starts after selection", async () => {
  const pane = draw();
  await selectAll();
  pane.live(["r1"]);
  expect((screen.getByRole("button", { name: "归档所选会话" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: "删除所选会话" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("所选会话正在运行，停止后才能操作")).toBeTruthy();
  expect(pane.onClose).not.toHaveBeenCalled();
  expect(pane.hub.removeSession).not.toHaveBeenCalled();
});

it("rechecks each running state during a batch instead of stopping a newly started turn", async () => {
  const pane = draw([sessions[0]!, { ...sessions[1]!, runtimeId: "r2" }]);
  pane.onArchive.mockImplementationOnce(async () => pane.live(["r2"]));
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "归档所选会话" }));
  await waitFor(() => expect(pane.onError).toHaveBeenCalled());
  expect(pane.onArchive).toHaveBeenCalledTimes(1);
  expect(pane.onClose).not.toHaveBeenCalled();
  expect(screen.getByRole("treeitem", { name: /Beta/ }).getAttribute("aria-selected")).toBe("true");
});

it("keeps a session when closing its pane is refused and still deletes the other selected row", async () => {
  const pane = draw();
  const failure = new Error("pane cannot close");
  const kept = draftKey("", "/w", "/sessions/a.jsonl");
  const removed = draftKey("", "/w", "/sessions/b.jsonl");
  const remote = draftKey("other-host", "/w", "/sessions/b.jsonl");
  writeDraft(kept, "keep the refused session");
  writeDraft(removed, "delete with the removed session");
  writeDraft(remote, "keep the remote session");
  pane.onClose.mockRejectedValueOnce(failure);
  await selectAll();
  await userEvent.click(screen.getByRole("button", { name: "删除所选会话" }));
  await userEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(pane.onError).toHaveBeenCalledWith(failure));
  expect(pane.hub.removeSession).toHaveBeenCalledTimes(1);
  expect(pane.hub.removeSession).toHaveBeenCalledWith("/sessions/b.jsonl");
  expect(screen.getByText("已选 1 个会话")).toBeTruthy();
  expect(readDraft(kept)).toBe("keep the refused session");
  expect(readDraft(removed)).toBe("");
  expect(readDraft(remote)).toBe("keep the remote session");
});

it("keeps a pending batch from being submitted twice", async () => {
  const pane = draw();
  let release: () => void = () => {};
  pane.onArchive.mockImplementationOnce(() => new Promise<void>((resolve) => { release = resolve; }));
  await selectAll();
  await userEvent.dblClick(screen.getByRole("button", { name: "归档所选会话" }));
  expect(pane.onArchive).toHaveBeenCalledTimes(1);
  expect((screen.getByRole("button", { name: "完成选择" }) as HTMLButtonElement).disabled).toBe(true);
  await act(async () => release());
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledTimes(2));
});

it("selects only displayed rows and clears the selection when the query changes", async () => {
  const many = Array.from({ length: 35 }, (_, i) => ({ path: `/sessions/${i}.jsonl`, name: `${i}`, title: `Task ${i}` }));
  const pane = draw(many);
  await selectAll();
  expect(screen.getByText("已选 30 个会话")).toBeTruthy();
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "Task 34");
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
  expect(pane.onArchive).not.toHaveBeenCalled();
});

it("drops deleted selections when a tree refresh removes their canonical rows", async () => {
  const pane = draw();
  await selectAll();
  pane.update([{ root: "/w", name: "Project", sessions: [sessions[1]!] }]);
  expect(screen.getByText("已选 1 个会话")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "归档所选会话" }));
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledWith("/sessions/b.jsonl", true, undefined));
  expect(pane.onArchive).toHaveBeenCalledTimes(1);
});

it("removes hidden selections when a project or the local machine is folded", async () => {
  const pane = draw();
  await selectAll();
  pane.update([
    { root: "/w", name: "Project", sessions },
    { root: "/other", name: "Other", sessions: [{ path: "/sessions/d.jsonl", name: "Delta" }] },
  ]);
  pane.fold(new Set(["/w"]));
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
  pane.fold(new Set());
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
  pane.fold(new Set(["/w"]));
  await userEvent.click(screen.getByRole("button", { name: "全选当前显示的会话" }));
  expect(screen.getByText("已选 1 个会话")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "取消当前显示会话的选择" }));
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "全选当前显示的会话" }));
  await userEvent.click(screen.getByRole("treeitem", { name: /这台机器/ }));
  expect(screen.getByText("已选 0 个会话")).toBeTruthy();
  expect((screen.getByRole("button", { name: "删除所选会话" }) as HTMLButtonElement).disabled).toBe(true);
  expect(pane.onArchive).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "完成选择" }));
  expect(document.activeElement).toBe(screen.getByRole("treeitem", { name: /这台机器/ }));
});

it("limits a search matching the project name to the rows actually rendered", async () => {
  draw(Array.from({ length: 35 }, (_, i) => ({ path: `/sessions/${i}.jsonl`, name: `${i}`, title: `Task ${i}` })));
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索会话 / 项目" }), "Project");
  await selectAll();
  expect(screen.getByText("已选 30 个会话")).toBeTruthy();
  expect(screen.queryByRole("treeitem", { name: /Task 34/ })).toBeNull();
});
