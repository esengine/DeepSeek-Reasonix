// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanFold } from "./PlanFold";
import { boot, STORAGE } from "../i18n";
import type { PlanStep } from "../state/session";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const plan: PlanStep[] = [
  { text: "移除失效门禁钩子", status: "in_progress" },
  { text: "清理运行时产物", status: "pending" },
  { text: "重写交接文档", status: "pending" },
];

describe("putting the open plan away", () => {
  it("offers a collapse button inside the open list, not only the bar's chevron", async () => {
    render(<PlanFold plan={plan} shown />);
    expect(screen.queryByRole("button", { name: "收起计划" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    const collapse = screen.getByRole("button", { name: "收起计划" });
    expect(collapse.closest(".studio-todo-body")).not.toBeNull();
    await userEvent.click(collapse);
    expect(screen.queryByRole("button", { name: "收起计划" })).toBeNull();
    expect(screen.getByRole("button", { expanded: false })).toBeTruthy();
  });

  it("closes the list on Escape and returns focus to the bar", async () => {
    render(<PlanFold plan={plan} shown />);
    const bar = screen.getByRole("button", { expanded: false });
    await userEvent.click(bar);
    expect(bar.getAttribute("aria-expanded")).toBe("true");
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("button", { expanded: false })).toBe(bar);
    expect(document.activeElement).toBe(bar);
  });

  it("titles the open list once, with where the plan stands", async () => {
    render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    const head = document.querySelector(".studio-todo-head")!;
    expect(head.textContent).toContain("计划 · 第 1 / 3 步");
    expect(head.textContent).not.toContain("待处理");
    expect(screen.queryByRole("button", { name: "跳到卡片 ↑" })).toBeNull();
  });

  it("takes the reader back to the plan's card and puts the list away", async () => {
    const onJump = vi.fn();
    render(<PlanFold plan={plan} shown onJump={onJump} />);
    const bar = screen.getByRole("button", { expanded: false });
    await userEvent.click(bar);
    await userEvent.click(screen.getByRole("button", { name: "跳到卡片 ↑" }));
    expect(onJump).toHaveBeenCalledTimes(1);
    expect(bar.getAttribute("aria-expanded")).toBe("false");
  });

  it("puts the list away when the press lands outside it, and keeps it for a press on the bar itself", async () => {
    render(<><PlanFold plan={plan} shown /><button type="button">别处</button></>);
    const bar = screen.getByRole("button", { expanded: false });
    await userEvent.click(bar);
    expect(bar.getAttribute("aria-expanded")).toBe("true");
    await userEvent.click(screen.getByText("清理运行时产物"));
    expect(bar.getAttribute("aria-expanded")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "别处" }));
    expect(bar.getAttribute("aria-expanded")).toBe("false");
  });
});
