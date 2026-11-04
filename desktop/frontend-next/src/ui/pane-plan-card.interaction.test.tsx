// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { HostTodo } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { WireEvent } from "../port/wire";

// Everything is near on observe, as in the shared stub; the card's own observer
// is kept so the test can scroll it away.
const watched = new Map<Element, IntersectionObserverCallback>();
const stub = globalThis.IntersectionObserver;
beforeAll(() => {
  globalThis.IntersectionObserver = class {
    readonly root = null;
    readonly rootMargin = "";
    readonly thresholds: readonly number[] = [];
    readonly scrollMargin = "";
    constructor(private readonly fn: IntersectionObserverCallback) {}
    observe(el: Element) {
      watched.set(el, this.fn);
      this.fn([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as never);
    }
    unobserve() {}
    disconnect() {}
    takeRecords() { return []; }
  } as unknown as typeof IntersectionObserver;
});
afterAll(() => { globalThis.IntersectionObserver = stub; });
afterEach(() => { cleanup(); vi.restoreAllMocks(); watched.clear(); });

it("shows the plan once: in its card while it is on screen, in the strip only once it scrolls away", async () => {
  const port = new MockPort();
  let todos: HostTodo[] = [];
  vi.spyOn(port, "todos").mockImplementation(async () => todos);
  // The bench's own decision runs a scripted turn to its end; the turn here is driven by the test.
  const decide = vi.spyOn(port, "planDecision").mockResolvedValue(undefined);
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
  const seed = [
    { content: "读出导出脚本", status: "in_progress" },
    { content: "改成按 GBK 解码", status: "pending" },
    { content: "跑全量测试", status: "pending" },
  ];
  await act(async () => emit({ kind: "approval_request", approval: { id: "a1", tool: "exit_plan_mode", subject: "", kind: "plan", input: { todos: seed } } } as WireEvent));
  const card = () => view.container.querySelector<HTMLElement>(".plan-card")!;
  await waitFor(() => expect(card()).not.toBeNull());
  expect([...card().querySelectorAll(".plan-step-mark")].map((m) => m.textContent)).toEqual(["1", "2", "3"]);
  expect(view.container.querySelector(".studio-todo")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "开始执行" }));
  await waitFor(() => expect(decide).toHaveBeenCalledWith("a1", "start"));
  todos = [{ content: "读出导出脚本", status: "completed" }, { content: "改成按 GBK 解码", status: "in_progress" }, { content: "跑全量测试", status: "pending" }];
  const st = await port.status();
  vi.spyOn(port, "status").mockResolvedValue({ ...st, running: true });
  await act(async () => emit({ kind: "turn_started" } as WireEvent));
  await act(async () => emit({ kind: "todo_progress" } as WireEvent));
  await waitFor(() => expect(card().querySelector(".plan-card-badge")?.textContent).toBe("第 2 / 3 步"));
  expect([...card().querySelectorAll(".plan-step")].map((row) => row.getAttribute("data-state"))).toEqual(["done", "now", "todo"]);
  expect(view.container.querySelector(".studio-todo")).toBeNull();

  const away = watched.get(card())!;
  const seen = (isIntersecting: boolean) => ({ isIntersecting, target: card() }) as unknown as IntersectionObserverEntry;
  await act(async () => away([seen(false)], {} as IntersectionObserver));
  const bar = await waitFor(() => view.container.querySelector<HTMLButtonElement>('.studio-todo [data-action="plan.fold"]')!);
  expect(bar.querySelector("b")?.textContent).toBe("计划 · 第 2 / 3 步");
  fireEvent.click(bar);
  expect(screen.getByRole("button", { name: "跳到卡片 ↑" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "跳到卡片 ↑" }));
  expect(bar.getAttribute("aria-expanded")).toBe("false");

  await act(async () => away([seen(true)], {} as IntersectionObserver));
  await waitFor(() => expect(view.container.querySelector(".studio-todo")).toBeNull());
});
