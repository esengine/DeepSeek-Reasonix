// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanFold } from "./PlanFold";
import { boot, STORAGE } from "../i18n";
import type { PlanStep } from "../state/session";

const plan: PlanStep[] = [
  { text: "确认插入位置与中文说明文案", status: "in_progress" },
  { text: "检查 git diff 仅含这一处改动", status: "pending" },
];

let running: { finished: Promise<unknown> }[] = [];
let finish: () => void = () => {};

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  running = [];
  Object.defineProperty(HTMLElement.prototype, "getAnimations", {
    configurable: true,
    value: () => running,
  });
});
afterEach(() => {
  cleanup();
  delete (HTMLElement.prototype as { getAnimations?: unknown }).getAnimations;
});

const body = () => document.querySelector(".studio-todo-body");
const strip = () => document.querySelector(".studio-todo");

function exitTakesTime() {
  const done = new Promise<void>((resolve) => { finish = resolve; });
  running = [{ finished: done }];
}

describe("the open plan leaving", () => {
  it("stays on screen, marked as leaving, until its exit animation has finished", async () => {
    render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    expect(body()?.hasAttribute("data-closing")).toBe(false);
    exitTakesTime();
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    expect(body()).not.toBeNull();
    expect(body()?.hasAttribute("data-closing")).toBe(true);
    expect(strip()?.hasAttribute("data-open")).toBe(true);
    await act(async () => { finish(); await Promise.resolve(); });
    expect(body()).toBeNull();
    expect(strip()?.hasAttribute("data-open")).toBe(false);
  });

  it("returns focus to the bar only once the list is gone", async () => {
    render(<PlanFold plan={plan} shown />);
    const bar = screen.getByRole("button", { expanded: false });
    await userEvent.click(bar);
    const focus = vi.spyOn(bar, "focus");
    exitTakesTime();
    await userEvent.keyboard("{Escape}");
    expect(body()).not.toBeNull();
    expect(focus).not.toHaveBeenCalled();
    await act(async () => { finish(); await Promise.resolve(); });
    expect(focus).toHaveBeenCalledTimes(1);
  });

  it("is not kept waiting by an animation that never runs", async () => {
    render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    running = [];
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    expect(body()).toBeNull();
  });

  it("leaves nothing behind when the strip goes away mid-exit", async () => {
    const view = render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    exitTakesTime();
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    view.rerender(<PlanFold plan={plan} shown={false} />);
    expect(strip()).toBeNull();
    await act(async () => { finish(); await Promise.resolve(); });
    view.rerender(<PlanFold plan={plan} shown />);
    expect(body()).toBeNull();
    expect(screen.getByRole("button", { expanded: false })).toBeTruthy();
  });

  it("settles when the exit animation is cancelled by reopening", async () => {
    render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    let cancel: (e: unknown) => void = () => {};
    running = [{ finished: new Promise((_, reject) => { cancel = reject; }) }];
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    await act(async () => { cancel(new DOMException("cancelled", "AbortError")); await Promise.resolve(); });
    expect(body()).not.toBeNull();
    expect(body()?.hasAttribute("data-closing")).toBe(false);
  });
});
