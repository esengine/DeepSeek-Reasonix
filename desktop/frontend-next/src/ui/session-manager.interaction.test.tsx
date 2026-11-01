// @vitest-environment jsdom
import "./testkit";
import { cleanup, render, screen, waitFor, within, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { SessionManager } from "./SessionManager";
import type { TreeWindow, TreeWorkspace } from "../port/hub";
import { draftKey, readDraft, writeDraft } from "./drafts";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

const sessions = [
  { path: "/s/a.jsonl", name: "a", title: "Alpha", workspaceRoot: "/w", runtimeId: "r1", lastActiveAt: 400 },
  { path: "/s/b.jsonl", name: "b", title: "Beta", workspaceRoot: "/w", lastActiveAt: 300 },
  { path: "/s/c.jsonl", name: "c", title: "Gamma", workspaceRoot: "/w", archived: true, lastActiveAt: 200 },
  { path: "/s/d.jsonl", name: "d", title: "Delta", workspaceRoot: "/other", lastActiveAt: 100 },
];

function page(window: TreeWindow | undefined, rows = sessions): TreeWorkspace[] {
  const offset = window?.offset ?? 0;
  const q = window?.q?.trim().toLowerCase() ?? "";
  const matched = rows.filter((row) => !q || row.title.toLowerCase().includes(q) || row.workspaceRoot!.includes(q));
  const shown = matched.slice(offset, offset + (window?.limit ?? 50));
  const tree = [
    { root: "/w", name: "Project", sessions: shown.filter((row) => row.workspaceRoot === "/w") },
    { root: "/other", name: "Other", sessions: shown.filter((row) => row.workspaceRoot === "/other") },
  ].filter((ws) => ws.sessions.length > 0 || matched.length > 0);
  return tree.map((ws) => ({ ...ws, recentOffset: offset, remaining: Math.max(0, matched.length - offset - shown.length) }));
}

function draw(options: { rows?: typeof sessions; live?: string[] } = {}) {
  const asked: (TreeWindow | undefined)[] = [];
  const hub = {
    tree: vi.fn(async (window?: TreeWindow) => {
      asked.push(window);
      return page(window, options.rows ?? sessions);
    }),
    removeSession: vi.fn().mockResolvedValue(undefined),
  };
  const onArchive = vi.fn().mockResolvedValue(undefined);
  const onClosePanes = vi.fn().mockResolvedValue(undefined);
  const onChanged = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  const onError = vi.fn();
  let live = options.live ?? [];
  const renderIt = () => <SessionManager hub={hub as never} liveIds={(ids) => ids.filter((id) => live.includes(id))}
    onClosePanes={onClosePanes} onArchive={onArchive} onChanged={onChanged} onClose={onClose} onError={onError} />;
  const view = render(renderIt());
  return {
    hub, onArchive, onClosePanes, onChanged, onClose, onError, asked,
    setLive: (ids: string[]) => { live = ids; view.rerender(renderIt()); },
  };
}

it("lists local sessions across projects, running and archived ones included, with no selection yet", async () => {
  draw();
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  expect(await dialog.findByRole("checkbox", { name: "选择会话：Alpha" })).toBeTruthy();
  expect(dialog.getByRole("checkbox", { name: "选择会话：Delta" })).toBeTruthy();
  expect(dialog.getAllByText("Project").length).toBe(2);
  expect(dialog.getAllByText("Other").length).toBe(1);
  expect(dialog.getByText("Project · 已归档")).toBeTruthy();
  expect(dialog.getByText("已选 0 个会话")).toBeTruthy();
  expect((dialog.getByRole("button", { name: "删除所选会话" }) as HTMLButtonElement).disabled).toBe(true);
});

it("selects the whole page, names the rows before deleting, and cancels with Escape without a mutation", async () => {
  const pane = draw();
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  expect(dialog.getByText("已选 4 个会话")).toBeTruthy();
  await userEvent.click(dialog.getByRole("button", { name: "删除所选会话" }));
  const question = await dialog.findByRole("alertdialog", { name: "删除 4 个会话？" });
  expect(within(question).getByText("Alpha、Beta、Gamma、Delta")).toBeTruthy();
  expect(pane.hub.removeSession).not.toHaveBeenCalled();
  await userEvent.keyboard("{Escape}");
  expect(dialog.queryByRole("alertdialog")).toBeNull();
  expect(screen.getByRole("dialog", { name: "管理会话" })).toBeTruthy();
  expect(document.activeElement).toBe(dialog.getByRole("button", { name: "删除所选会话" }));
  expect(pane.hub.removeSession).not.toHaveBeenCalled();

  await userEvent.click(dialog.getByRole("button", { name: "删除所选会话" }));
  await userEvent.click(within(await dialog.findByRole("alertdialog")).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(pane.hub.removeSession).toHaveBeenCalledTimes(4));
  expect(pane.onClosePanes).toHaveBeenCalledWith(["r1"]);
  expect(pane.onClosePanes.mock.invocationCallOrder[0]).toBeLessThan(pane.hub.removeSession.mock.invocationCallOrder[0]!);
  expect(pane.onChanged).toHaveBeenCalledTimes(1);
  await waitFor(() => expect(dialog.getByText("已选 0 个会话")).toBeTruthy());
});

it("clears the draft of every deleted session and keeps the remote draft of the same path", async () => {
  const pane = draw();
  const kept = draftKey("", "/w", "/s/a.jsonl");
  const dropped = draftKey("", "/w", "/s/b.jsonl");
  const remote = draftKey("other-host", "/w", "/s/b.jsonl");
  writeDraft(kept, "keep the running session");
  writeDraft(dropped, "delete with the session");
  writeDraft(remote, "keep the remote session");
  pane.setLive(["r1"]);
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("checkbox", { name: "选择会话：Beta" }));
  await userEvent.click(dialog.getByRole("button", { name: "删除所选会话" }));
  await userEvent.click(within(await dialog.findByRole("alertdialog")).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(pane.hub.removeSession).toHaveBeenCalledWith("/s/b.jsonl"));
  expect(readDraft(kept)).toBe("keep the running session");
  expect(readDraft(dropped)).toBe("");
  expect(readDraft(remote)).toBe("keep the remote session");
});

it("refuses to archive or delete while a selected session is running", async () => {
  const pane = draw();
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  pane.setLive(["r1"]);
  expect(dialog.getByText("所选会话正在运行，停止后才能操作")).toBeTruthy();
  expect((dialog.getByRole("button", { name: "归档所选会话" }) as HTMLButtonElement).disabled).toBe(true);
  expect((dialog.getByRole("button", { name: "删除所选会话" }) as HTMLButtonElement).disabled).toBe(true);
  expect(pane.onClosePanes).not.toHaveBeenCalled();
  expect(pane.hub.removeSession).not.toHaveBeenCalled();
});

it("keeps a failed row selected after a batch and reports the failure", async () => {
  const pane = draw();
  const failure = new Error("storage unavailable");
  pane.hub.removeSession.mockRejectedValueOnce(failure);
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  await userEvent.click(dialog.getByRole("button", { name: "删除所选会话" }));
  await userEvent.click(within(await dialog.findByRole("alertdialog")).getByRole("button", { name: "删除" }));
  await waitFor(() => expect(pane.onError).toHaveBeenCalledWith(failure));
  expect(pane.hub.removeSession).toHaveBeenCalledTimes(4);
  expect(dialog.getByText("已选 1 个会话")).toBeTruthy();
  expect((dialog.getByRole("checkbox", { name: "选择会话：Alpha" }) as HTMLInputElement).checked).toBe(true);
  expect((dialog.getByRole("checkbox", { name: "选择会话：Beta" }) as HTMLInputElement).checked).toBe(false);
});

it("archives and restores selected rows through the owner the sidebar uses", async () => {
  const pane = draw();
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("checkbox", { name: "选择会话：Gamma" }));
  expect(dialog.getByRole("button", { name: "取消所选会话归档" })).toBeTruthy();
  await userEvent.click(dialog.getByRole("button", { name: "取消所选会话归档" }));
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledWith("/s/c.jsonl", false, undefined));
  expect(pane.onChanged).not.toHaveBeenCalled();
  expect(pane.asked.length).toBeGreaterThan(1);
});

it("starts a new selection on a new page and asks the kernel for that page", async () => {
  const many = Array.from({ length: 60 }, (_, i) => ({
    path: `/s/${i}.jsonl`, name: `${i}`, title: `Task ${i}`, workspaceRoot: "/w", lastActiveAt: 1000 - i,
  }));
  const pane = draw({ rows: many });
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  expect(dialog.getByRole("button", { name: "上一页" })).toHaveProperty("disabled", true);
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  expect(dialog.getByText("已选 50 个会话")).toBeTruthy();
  await userEvent.click(dialog.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(pane.asked.at(-1)).toMatchObject({ order: "recent", scope: "", limit: 50, offset: 50 }));
  expect(dialog.getByText("已选 0 个会话")).toBeTruthy();
  expect(dialog.getByRole("button", { name: "上一页" })).toHaveProperty("disabled", false);
  expect(dialog.getByRole("button", { name: "下一页" })).toHaveProperty("disabled", true);
});

it("filters in the kernel and drops the selection with the old result set", async () => {
  const pane = draw();
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  expect(dialog.getByText("已选 4 个会话")).toBeTruthy();
  await userEvent.type(dialog.getByRole("searchbox", { name: "搜索会话 / 项目" }), "Beta");
  await waitFor(() => expect(pane.asked.at(-1)).toMatchObject({ q: "Beta", offset: 0 }));
  await waitFor(() => expect(dialog.getByText("已选 0 个会话")).toBeTruthy());
  await waitFor(() => expect(dialog.getByRole("checkbox", { name: "选择会话：Beta" })).toBeTruthy());
  expect(dialog.queryByRole("checkbox", { name: "选择会话：Alpha" })).toBeNull();
});

it("keeps a pending batch from being submitted twice", async () => {
  const pane = draw();
  let release: () => void = () => {};
  pane.onArchive.mockImplementationOnce(() => new Promise<void>((resolve) => { release = resolve; }));
  const dialog = within(await screen.findByRole("dialog", { name: "管理会话" }));
  await userEvent.click(dialog.getByRole("button", { name: "全选本页" }));
  await userEvent.dblClick(dialog.getByRole("button", { name: "归档所选会话" }));
  expect(pane.onArchive).toHaveBeenCalledTimes(1);
  await act(async () => release());
  await waitFor(() => expect(pane.onArchive).toHaveBeenCalledTimes(4));
});

it("closes the sheet on Escape when no confirmation is open", async () => {
  const pane = draw();
  await screen.findByRole("dialog", { name: "管理会话" });
  await userEvent.keyboard("{Escape}");
  expect(pane.onClose).toHaveBeenCalledOnce();
});
