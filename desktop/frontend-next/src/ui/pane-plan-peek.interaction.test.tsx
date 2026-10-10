// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { HostTodo } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { WireEvent } from "../port/wire";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("reads the host plan after reload, retains the composer entry, and follows todo progress", async () => {
  const port = new MockPort();
  let todos: HostTodo[] = [
    { content: "复现问题", status: "completed" },
    { content: "修复问题", status: "in_progress", activeForm: "正在修复问题" },
    { content: "验证结果", status: "pending" },
  ];
  vi.spyOn(port, "todos").mockImplementation(async () => todos);
  const st = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...st, goal: "验证当前修复" });
  let emit: (event: WireEvent) => void = () => {};
  const subscribe = port.subscribe.bind(port);
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
    emit = onEvent;
    return subscribe(onEvent, onGap, bootstrap);
  });
  const view = render(<Pane port={port} rt={{ id: "p1", root: "/w", name: "w" } as RuntimeView}
    title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />);
  await waitFor(() => expect(view.container.querySelector('.plan-peek-trigger')?.textContent).toContain("1/3"));
  const composerPlan = view.container.querySelector<HTMLButtonElement>('.studio-todo [data-action="plan.fold"]')!;
  fireEvent.click(composerPlan);
  expect(view.container.querySelector(".studio-todo-body")?.textContent).toContain("正在修复问题");
  fireEvent.click(view.container.querySelector('.plan-peek-trigger')!);
  expect(screen.getByRole("dialog", { name: "计划" }).textContent).toContain("验证当前修复");
  expect(screen.getByRole("dialog", { name: "计划" }).textContent).toContain("正在修复问题");
  expect(view.container.querySelector('[data-value="flow"]')?.getAttribute("aria-selected")).toBe("true");
  todos = todos.map((todo, index) => ({ ...todo, status: index < 2 ? "completed" : "in_progress" }));
  await act(async () => emit({ kind: "todo_progress" } as WireEvent));
  await waitFor(() => expect(screen.getByRole("dialog", { name: "计划" }).querySelectorAll(".s[data-done]")).toHaveLength(2));
  expect(view.container.querySelector('.plan-peek-trigger')?.textContent).toContain("2/3");
  expect(view.container.querySelector(".studio-todo-body")?.textContent).toContain("修复问题");
  expect(view.container.querySelector(".studio-todo-body")?.textContent).not.toContain("正在修复问题");
  todos = todos.map((todo) => ({ ...todo, status: "completed" }));
  await act(async () => emit({ kind: "todo_progress" } as WireEvent));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "计划" })).toBeNull());
  expect(screen.getByRole("dialog", { name: "目标" }).textContent).toContain("验证当前修复");
  expect(view.container.querySelector(".studio-todo")).toBeNull();
});
