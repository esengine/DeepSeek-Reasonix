// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { PaneNav } from "./PaneNav";
import type { PlanStep } from "../state/session";

afterEach(cleanup);

const plan: PlanStep[] = [
  { text: "复现问题", status: "completed" },
  { text: "修复问题", status: "in_progress", activeForm: "正在修复问题" },
  { text: "检查结果", status: "pending" },
];

const draw = (rows = 3, surfaces = 0) => {
  const onPick = vi.fn();
  render(<PaneNav view="flow" onPick={onPick} rows={rows} surfaces={surfaces} />);
  return onPick;
};

describe("pane navigation", () => {
  it("opens the current goal and plan without leaving the conversation", () => {
    const onPick = vi.fn();
    const view = render(<PaneNav view="flow" onPick={onPick} rows={3} surfaces={1} plan={plan} goal="修复当前问题" />);
    const trigger = screen.getByRole("button", { name: /计划/ });
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.textContent).toContain("1/3");
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "计划" });
    expect(dialog.textContent).toContain("修复当前问题");
    expect(dialog.textContent).toContain("正在修复问题");
    expect(dialog.querySelectorAll(".s")).toHaveLength(3);
    expect(view.container.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe("对话");
    expect(onPick).not.toHaveBeenCalled();
  });

  it("projects live plan updates and closes when the current task disappears", () => {
    const props = { view: "flow" as const, onPick: vi.fn(), rows: 0, surfaces: 0 };
    const view = render(<PaneNav {...props} plan={plan} />);
    fireEvent.click(screen.getByRole("button", { name: /计划/ }));
    view.rerender(<PaneNav {...props} plan={plan.map((step) => ({ ...step, status: "completed" }))} />);
    expect(screen.getByRole("button", { name: /计划/ }).textContent).toContain("3/3");
    expect(screen.getByRole("dialog").querySelectorAll(".s[data-done]")).toHaveLength(3);
    expect(screen.getByRole("dialog").querySelector("[data-now]")).toBeNull();
    view.rerender(<PaneNav {...props} plan={[]} />);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("button", { name: /计划/ })).toBeNull();
    view.rerender(<PaneNav {...props} plan={plan} />);
    expect(screen.getByRole("button", { name: /计划/ }).getAttribute("aria-expanded")).toBe("false");
  });

  it("dismisses on Escape with focus return and on outside press", () => {
    render(<><PaneNav view="flow" onPick={vi.fn()} rows={0} surfaces={0} plan={plan} /><button>outside</button></>);
    const trigger = screen.getByRole("button", { name: /计划/ });
    fireEvent.click(trigger);
    expect(document.activeElement).toBe(screen.getByRole("dialog"));
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(trigger);
    fireEvent.click(trigger);
    fireEvent.mouseDown(screen.getByRole("dialog"));
    expect(screen.getByRole("dialog")).toBeTruthy();
    fireEvent.mouseDown(screen.getByRole("button", { name: "outside" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the plan control outside arrow-key tab navigation and supports toggling closed", () => {
    const onPick = vi.fn();
    render(<PaneNav view="flow" onPick={onPick} rows={3} surfaces={0} plan={plan} />);
    const trigger = screen.getByRole("button", { name: /计划/ });
    screen.getByRole("tab", { name: "工作台" }).focus();
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "工作台" }));
    expect(onPick).not.toHaveBeenCalled();
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "运行分析" }));
    expect(onPick).toHaveBeenCalledExactlyOnceWith("analysis");
    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers a canonical goal before a plan exists and no extra control for an empty task", () => {
    const props = { view: "flow" as const, onPick: vi.fn(), rows: 0, surfaces: 0 };
    const view = render(<PaneNav {...props} goal="定位测试失败" />);
    fireEvent.click(screen.getByRole("button", { name: "目标" }));
    expect(screen.getByRole("dialog", { name: "目标" }).textContent).toContain("定位测试失败");
    view.rerender(<PaneNav {...props} goal="" />);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("keeps only conversation, the readable run analysis and the workbench", () => {
    draw();
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["对话", "运行分析", "工作台"]);
    expect(screen.queryByText("任务")).toBeNull();
    expect(screen.queryByText("运行详情")).toBeNull();
  });

  it("opens analysis directly", () => {
    const onPick = draw(2, 1);
    fireEvent.click(screen.getByRole("tab", { name: "运行分析" }));
    expect(onPick).toHaveBeenCalledWith("analysis");
  });

  // The count said how many surfaces the workbench holds while carrying a +1
  // that kept the tab drawn, so one open browser was announced as two.
  it("counts the surfaces the workbench strip holds, and nothing else", () => {
    draw(0, 0);
    expect(screen.getByRole("tab", { name: "工作台" }).querySelector(".n")).toBeNull();
    cleanup();
    draw(0, 1);
    expect(screen.getByRole("tab", { name: /工作台/ }).querySelector(".n")?.textContent).toBe("1");
  });
});
