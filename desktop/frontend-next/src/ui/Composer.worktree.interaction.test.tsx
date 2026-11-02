// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { Composer } from "./Composer";
import { MockPort } from "../port/mock";
import type { SessionStatus } from "../port/port";
import type { WorktreePreparation } from "../port/workspace";

afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

it.each(["failed", "cancelled"])("restores composer focus after dismissing %s preparation", async (phase) => {
  const port = new MockPort();
  vi.spyOn(port, "worktreeSources").mockResolvedValue({ available: true, sourceRoot: "/source", sources: [{ ref: "refs/heads/main", name: "main", current: true, remote: false }] });
  let resolve!: (value: WorktreePreparation) => void;
  const start = vi.spyOn(port, "prepareWorktree").mockImplementation(() => new Promise((done) => { resolve = done; }));
  const onSubmit = vi.fn(async () => true);
  const st = { workspaceRoot: "/source", modelRef: "deepseek/deepseek-v4-pro", preset: "balanced", effort: "auto", toolApprovalMode: "ask" } as SessionStatus;
  render(<Composer port={port} status={st} running={false} fresh git={{ repo: true, branch: "main", name: "project", detached: false, added: 0, removed: 0, untracked: 0 }} onSubmit={onSubmit} onChanged={vi.fn()} onError={vi.fn()}/>);
  fireEvent.click(await screen.findByRole("button", { name: "配置工作目录" }));
  fireEvent.click(screen.getByRole("button", { name: "独立工作树" }));
  fireEvent.click(screen.getByRole("button", { name: "关闭工作目录配置" }));
  const box = screen.getByLabelText("任务输入") as HTMLTextAreaElement;
  box.focus();
  // jsdom lacks inert focus suppression; mirror the browser boundary locally.
  const focus = box.focus.bind(box);
  vi.spyOn(box, "focus").mockImplementation((options) => { if (!box.closest("[inert]")) focus(options); });
  fireEvent.change(box, { target: { value: "保留这份任务", selectionStart: 3 } });
  box.blur();
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(start).toHaveBeenCalledOnce());
  await act(async () => resolve({ id: start.mock.calls[0][0], phase, sourceRoot: "/source", ref: "refs/heads/main", startedAt: Date.now() }));
  fireEvent.click(await screen.findByRole("button", { name: "返回编辑" }));
  await waitFor(() => expect(document.activeElement).toBe(box));
  expect(box.closest("[inert]")).toBeNull();
  expect(box.value).toBe("保留这份任务");
  expect(onSubmit).not.toHaveBeenCalled();
});
