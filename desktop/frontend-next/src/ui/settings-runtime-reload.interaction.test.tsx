// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, SessionStatus } from "../port/port";

afterEach(cleanup);

function pendingReload(port: AgentPort) {
  let finish!: () => void;
  let fail!: (error: Error) => void;
  const reload = vi.spyOn(port, "reloadExtensions").mockImplementationOnce(() => new Promise<void>((resolve, reject) => {
    finish = resolve;
    fail = reject;
  }));
  return { reload, settle: (outcome: string) => outcome === "success" ? finish() : fail(new Error("old reload failed")) };
}

function draw(port: AgentPort, onChanged: () => void) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

function expectReady() {
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "重载运行时" }).disabled).toBe(false);
  expect(screen.queryByText("正在重启常驻进程，重新扫描技能、命令和钩子…")).toBeNull();
  expect(screen.queryByText("已生效，下一轮开始用新的扩展")).toBeNull();
  expect(screen.queryByText("old reload failed")).toBeNull();
}

it.each(["success", "failure"])("isolates a pending reload %s after Settings changes connection", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = pendingReload(port);
  const current = pendingReload(next);
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await userEvent.click(screen.getByRole("button", { name: "重载运行时" }));
  expect(old.reload).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "重载中" }).disabled).toBe(true);
  view.rerender(draw(next, onChanged));
  expectReady();
  await userEvent.click(screen.getByRole("button", { name: "重载运行时" }));
  const applying = screen.getByRole<HTMLButtonElement>("button", { name: "重载中" });
  expect(current.reload).toHaveBeenCalledTimes(1);
  await act(async () => old.settle(outcome));
  expect(applying.disabled).toBe(true);
  expect(screen.getByText("正在重启常驻进程，重新扫描技能、命令和钩子…")).toBeTruthy();
  expect(screen.queryByText("old reload failed")).toBeNull();
  expect(screen.queryByText("已生效，下一轮开始用新的扩展")).toBeNull();
  expect(onChanged).not.toHaveBeenCalled();
  await userEvent.click(applying);
  expect(current.reload).toHaveBeenCalledTimes(1);
  await act(async () => current.settle("success"));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "已生效" }).disabled).toBe(false);
  expect(screen.getByText("已生效，下一轮开始用新的扩展")).toBeTruthy();
  expect(onChanged).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("clears a settled reload %s when Settings changes connection", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = pendingReload(port);
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await userEvent.click(screen.getByRole("button", { name: "重载运行时" }));
  await act(async () => old.settle(outcome));
  expect(screen.getByText(outcome === "success" ? "已生效，下一轮开始用新的扩展" : "old reload failed")).toBeTruthy();
  view.rerender(draw(next, onChanged));
  expectReady();
});

it.each(["success", "failure"])("keeps a same-connection reload pending across a Settings refresh (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = pendingReload(port);
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await userEvent.click(screen.getByRole("button", { name: "重载运行时" }));
  view.rerender(draw(port, onChanged));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "重载中" }).disabled).toBe(true);
  await act(async () => pending.settle(outcome));
  expect(screen.getByText(outcome === "success" ? "已生效，下一轮开始用新的扩展" : "old reload failed")).toBeTruthy();
  expect(onChanged).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
});

it.each(["success", "failure"])("ignores a previous connection lifetime's reload after returning to the port (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = pendingReload(port);
  const onChanged = vi.fn();
  const view = render(draw(port, onChanged));
  await userEvent.click(screen.getByRole("button", { name: "重载运行时" }));
  view.rerender(draw(next, onChanged));
  view.rerender(draw(port, onChanged));
  await act(async () => old.settle(outcome));
  expectReady();
  expect(onChanged).not.toHaveBeenCalled();
});
