// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { PaneNav } from "./PaneNav";

afterEach(cleanup);

const draw = (rows = 3, surfaces = 0) => {
  const onPick = vi.fn();
  render(<PaneNav view="flow" onPick={onPick} rows={rows} surfaces={surfaces} />);
  return onPick;
};

describe("pane navigation", () => {
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
