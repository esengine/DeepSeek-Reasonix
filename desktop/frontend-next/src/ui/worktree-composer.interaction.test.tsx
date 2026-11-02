// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { SessionStatus } from "../port/port";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function draw(fresh = true, branch = "main", isolated = false) {
  const port = new MockPort();
  const readSources = port.worktreeSources.bind(port);
  vi.spyOn(port, "worktreeSources").mockImplementation(async () => ({ ...await readSources(), sourceRoot: "/work/demo" }));
  vi.spyOn(port, "status").mockResolvedValue({ workspaceRoot: "~/projects/DeepSeek-Reasonix" } as SessionStatus);
  const workspace = { current: "/work/demo", canSwitch: true, canIsolate: true, recents: [], isolated };
  const isolate = vi.spyOn(port, "isolateWorkspace").mockResolvedValue();
  const submit = vi.fn(async () => true);
  const error = vi.fn();
  const status = { workspaceRoot: "/work/demo", modelRef: "deepseek/deepseek-v4-pro", preset: "balanced", toolApprovalMode: "ask", plan: false } as SessionStatus;
  render(<Composer port={port} workspace={workspace} status={status} running={false} fresh={fresh} git={{ repo: true, name: "demo", branch, detached: false, added: 0, removed: 0, untracked: 0 }} onSubmit={submit} onChanged={vi.fn()} onTreeChanged={vi.fn()} onError={error} />);
  return { port, isolate, submit, error };
}

async function choose() {
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  fireEvent.click(await screen.findByRole("button", { name: "独立工作树" }));
  fireEvent.change(screen.getByRole("searchbox"), { target: { value: "studio" } });
  fireEvent.click(screen.getByRole("option", { name: /studio/ }));
  fireEvent.keyDown(window, { key: "Escape" });
  fireEvent.change(screen.getByRole("combobox", { name: "任务输入" }), { target: { value: "添加搜索功能" } });
}

it("selects a source without switching the checkout and creates only when sending", async () => {
  const { port, isolate, submit } = draw();
  const switchBranch = vi.spyOn(port, "switchBranch");
  const trigger = await screen.findByRole("button", { name: "配置工作目录" });
  expect(trigger.closest(".turntools")).not.toBeNull();
  expect(trigger.textContent).toBe("main");
  expect(trigger.title).toContain("直接在已打开的项目文件夹中工作");
  expect(trigger.title).toContain("/work/demo");
  await choose();
  expect(trigger.textContent).toContain("独立工作树 · studio");
  expect(trigger.title).toContain("发送消息时创建独立工作树");
  expect(isolate).not.toHaveBeenCalled();
  expect(switchBranch).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(submit).toHaveBeenCalledWith("添加搜索功能"));
  expect(isolate).toHaveBeenCalledExactlyOnceWith({ ref: "refs/heads/studio", sourceRoot: "/work/demo" });
  expect(isolate.mock.invocationCallOrder[0]).toBeLessThan(submit.mock.invocationCallOrder[0]);
});

it("restores the draft on creation failure and retries without submitting in the source", async () => {
  const { isolate, submit } = draw();
  isolate.mockRejectedValueOnce(new Error("disk full"));
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect((screen.getByLabelText("任务输入") as HTMLTextAreaElement).value).toBe("添加搜索功能"));
  expect(submit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "重试创建" }));
  await waitFor(() => expect(submit).toHaveBeenCalledOnce());
  expect(isolate).toHaveBeenCalledTimes(2);
});

it("reuses the prepared worktree when the first submission is refused", async () => {
  const { isolate, submit } = draw();
  submit.mockResolvedValueOnce(false);
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect((screen.getByLabelText("任务输入") as HTMLTextAreaElement).value).toBe("添加搜索功能"));
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(submit).toHaveBeenCalledTimes(2));
  expect(isolate).toHaveBeenCalledOnce();
});

it("keeps workspace configuration and branch switching in existing conversations", async () => {
  const { port } = draw(false);
  const move = vi.spyOn(port, "switchBranch");
  const trigger = await screen.findByRole("button", { name: "配置工作目录" });
  expect(trigger.closest(".turntools")).not.toBeNull();
  fireEvent.click(trigger);
  expect(screen.getByLabelText("查看完整路径")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "独立工作树" })).toBeNull();
  fireEvent.click(await screen.findByRole("option", { name: /studio/ }));
  await waitFor(() => expect(move).toHaveBeenCalledWith("studio"));
});

it("shows a completed worktree's actual identity, full branch and path", async () => {
  const branch = "reasonix/delivery-20261017-170557-387acbfc21";
  draw(false, branch, true);
  const trigger = await screen.findByRole("button", { name: "配置工作目录" });
  await waitFor(() => expect(trigger.textContent).toContain("独立工作树"));
  expect(trigger.title).toContain(branch);
  expect(trigger.title).toContain("在单独的 Git 工作目录中修改代码；端口、数据库等运行资源仍可能共享。");
  expect(trigger.title).toContain("/work/demo");
  fireEvent.click(trigger);
  expect(screen.getByLabelText("查看完整路径")).toBeTruthy();
  expect(screen.getByRole("searchbox", { name: "搜索分支" })).toBeTruthy();
  expect(screen.getByText("选择后立即切换，同目录会话也会受影响")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "独立工作树" })).toBeNull();
});

it("shows real preparation state and restores the draft only after confirmed cancellation", async () => {
  const { port, submit } = draw();
  let phase = "checking_out";
  const reading = { id: "", phase, sourceRoot: "~/projects/DeepSeek-Reasonix", ref: "refs/heads/studio", startedAt: Date.now(), branch: "reasonix/task", workspaceRoot: "/isolated/project" };
  vi.spyOn(port, "prepareWorktree").mockImplementation(async (id) => ({ ...reading, id }));
  vi.spyOn(port, "worktreePreparation").mockImplementation(async (id) => ({ ...reading, id, phase }));
  const cancel = vi.spyOn(port, "cancelWorktreePreparation").mockImplementation(async (id) => { phase = "cancelled"; return { ...reading, id, phase }; });
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await screen.findByRole("heading", { name: "正在创建 worktree…" });
  expect(await screen.findByText("正在检出已提交文件")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "取消创建" }));
  await screen.findByRole("heading", { name: "已取消创建" });
  await waitFor(() => expect((screen.getByLabelText("任务输入") as HTMLTextAreaElement).value).toBe("添加搜索功能"));
  expect(cancel).toHaveBeenCalledOnce();
  expect(submit).not.toHaveBeenCalled();
});

it("queries a lost acknowledgement before retrying and submits once in the prepared workspace", async () => {
  const { port, submit } = draw();
  const start = vi.spyOn(port, "prepareWorktree").mockRejectedValue(new Error("connection lost"));
  const read = vi.spyOn(port, "worktreePreparation").mockImplementation(async (id) => ({ id, phase: "ready", sourceRoot: "~/projects/DeepSeek-Reasonix", ref: "refs/heads/studio", startedAt: Date.now(), branch: "reasonix/task", workspaceRoot: "/isolated/project" }));
  await choose();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(submit).toHaveBeenCalledOnce());
  expect(start).toHaveBeenCalledOnce();
  expect(read).toHaveBeenCalledWith(start.mock.calls[0][0]);
});

it("offers local branch switching while worktree mode is off", async () => {
  const { port } = draw();
  const move = vi.spyOn(port, "switchBranch");
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  fireEvent.click(await screen.findByRole("option", { name: /studio/ }));
  await waitFor(() => expect(move).toHaveBeenCalledWith("studio"));
});

it("defaults a new worktree to the actual branch after an earlier branch switch", async () => {
  draw(true, "studio");
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  fireEvent.click(await screen.findByRole("button", { name: "独立工作树" }));
  expect(screen.getByRole("option", { name: "studio" }).getAttribute("aria-selected")).toBe("true");
});

it("keeps both directory choices in the picker without adding a default badge or creating a checkout", async () => {
  const { isolate, port } = draw();
  const move = vi.spyOn(port, "switchBranch");
  const trigger = await screen.findByRole("button", { name: "配置工作目录" });
  fireEvent.click(trigger);
  const toggle = screen.getByRole("button", { name: "独立工作树" });
  const current = screen.getByRole("button", { name: "当前目录" });
  expect(toggle.getAttribute("aria-pressed")).toBe("false");
  expect(current.getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByText("项目目录")).toBeNull();
  expect(document.querySelector(".workspace-palette-identity > span")).toBeNull();
  fireEvent.click(toggle);
  expect(toggle.getAttribute("aria-pressed")).toBe("true");
  expect(current.getAttribute("aria-pressed")).toBe("false");
  expect(trigger.textContent).toBe("独立工作树 · main");
  fireEvent.click(toggle);
  expect(toggle.getAttribute("aria-pressed")).toBe("true");
  fireEvent.click(current);
  expect(toggle.getAttribute("aria-pressed")).toBe("false");
  expect(current.getAttribute("aria-pressed")).toBe("true");
  expect(trigger.textContent).toBe("main");
  expect(trigger.title).toContain("直接在已打开的项目文件夹中工作");
  expect(screen.queryByText("项目目录")).toBeNull();
  expect(document.querySelector(".workspace-palette-identity > span")).toBeNull();
  expect(isolate).not.toHaveBeenCalled();
  expect(move).not.toHaveBeenCalled();
});

it("searches branches immediately and prevents switching an occupied branch", async () => {
  const { port } = draw(false);
  const move = vi.spyOn(port, "switchBranch");
  vi.spyOn(port, "branches").mockResolvedValue({ repo: true, branches: [{ name: "main", current: true }, { name: "studio", current: false, worktree: "/other/tree" }] });
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  const occupied = await screen.findByRole("option", { name: "studio" });
  expect((occupied as HTMLButtonElement).disabled).toBe(true);
  expect(occupied.title).toContain("/other/tree");
  fireEvent.change(screen.getByRole("searchbox"), { target: { value: "studio" } });
  expect(screen.queryByRole("option", { name: "main" })).toBeNull();
  fireEvent.click(occupied);
  expect(move).not.toHaveBeenCalled();
  fireEvent.change(screen.getByRole("searchbox"), { target: { value: "missing" } });
  expect(screen.getByText("没有匹配的分支")).toBeTruthy();
});

it("reveals and copies the full path without expanding it by default", async () => {
  draw(false);
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  const disclosure = screen.getByLabelText("查看完整路径").closest("details")!;
  expect(disclosure.open).toBe(false);
  fireEvent.click(screen.getByLabelText("查看完整路径"));
  expect(disclosure.open).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "复制完整路径" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("/work/demo"));
  vi.unstubAllGlobals();
});

it("recovers a failed branch read without treating it as an empty search", async () => {
  const { port } = draw(false);
  vi.spyOn(port, "branches").mockRejectedValueOnce(new Error("offline"));
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  await screen.findByRole("alert");
  expect(screen.queryByText("没有匹配的分支")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "重新读取" }));
  await screen.findByRole("option", { name: "studio" });
});

it("skips occupied branches with the keyboard and returns focus on Escape", async () => {
  const { port } = draw(false);
  vi.spyOn(port, "branches").mockResolvedValue({ repo: true, branches: [{ name: "main", current: true }, { name: "blocked", current: false, worktree: "/other" }, { name: "studio", current: false }] });
  const trigger = await screen.findByRole("button", { name: "配置工作目录" });
  fireEvent.click(trigger);
  await screen.findByRole("option", { name: "main" });
  const input = screen.getByRole("searchbox");
  expect(document.activeElement).toBe(input);
  fireEvent.keyDown(input, { key: "ArrowDown" });
  expect(document.activeElement).toBe(screen.getByRole("option", { name: "main" }));
  fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
  expect(document.activeElement).toBe(screen.getByRole("option", { name: "studio" }));
  fireEvent.keyDown(window, { key: "Escape" });
  expect(document.activeElement).toBe(trigger);
  expect(screen.queryByRole("dialog")).toBeNull();
});
