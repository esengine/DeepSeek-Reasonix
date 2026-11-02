// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanFold } from "./PlanFold";
import { PlanProgressSection } from "./PlanProgressSection";
import { boot, STORAGE } from "../i18n";
import { setShowsPlanProgress } from "../state/prefs";
import type { PlanStep } from "../state/session";

beforeEach(() => {
  localStorage.clear();
  localStorage.setItem(STORAGE, "zh");
  boot();
  setShowsPlanProgress(false);
});
afterEach(cleanup);

const plan: PlanStep[] = [
  { text: "Read requirements", status: "completed" },
  { text: "Implement changes", status: "in_progress" },
  { text: "Verify result", status: "pending" },
];

describe("optional progress below the collapsed plan", () => {
  it("applies the machine preference immediately to every mounted plan, and remembers it on remount", async () => {
    const settings = render(<PlanProgressSection />);
    const cards = render(<><PlanFold plan={plan} shown /><PlanFold plan={plan} shown /></>);
    expect(screen.queryAllByRole("progressbar")).toHaveLength(0);
    await userEvent.click(within(settings.container).getByRole("button", { name: "显示" }));
    expect(localStorage.getItem("rx-plan-progress")).toBe("on");
    expect(screen.getAllByRole("progressbar")).toHaveLength(2);
    const progress = screen.getAllByRole("progressbar")[0];
    expect(progress.getAttribute("aria-label")).toBe("计划进度");
    expect(progress.getAttribute("aria-valuenow")).toBe("1");
    expect(progress.getAttribute("aria-valuemax")).toBe("3");
    expect(progress.querySelector("i")?.style.width).toBe("33%");
    cards.unmount();
    render(<PlanFold plan={plan} shown />);
    expect(screen.getByRole("progressbar")).toBeTruthy();
    await userEvent.click(within(settings.container).getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(localStorage.getItem("rx-plan-progress")).toBe("off");
  });

  it("follows live canonical statuses, including resetting the plan and completing all steps", () => {
    setShowsPlanProgress(true);
    const view = render(<PlanFold plan={plan} shown paused />);
    for (const status of ["pending", "in_progress", "completed"] as const) {
      view.rerender(<PlanFold plan={plan.map((s) => ({ ...s, status }))} shown paused />);
      const progress = screen.getByRole("progressbar");
      expect(progress.getAttribute("aria-valuenow")).toBe(status === "completed" ? "3" : "0");
      expect(progress.querySelector("i")?.style.width).toBe(status === "completed" ? "100%" : "0%");
    }
  });

  it("keeps expanded progress and all collapse and dismiss interactions unchanged", async () => {
    setShowsPlanProgress(true);
    render(<><PlanFold plan={plan} shown /><button>Outside</button></>);
    const bar = screen.getByRole("button", { expanded: false });
    await userEvent.click(bar);
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(document.querySelector(".studio-todo-body .prog2 i")?.getAttribute("style")).toContain("33%");
    await userEvent.click(screen.getByText("Verify result"));
    expect(bar.getAttribute("aria-expanded")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    expect(screen.getByRole("progressbar")).toBeTruthy();
    expect(document.activeElement).toBe(bar);
    await userEvent.click(bar);
    await userEvent.keyboard("{Escape}");
    expect(screen.getByRole("progressbar")).toBeTruthy();
    expect(document.activeElement).toBe(bar);
    await userEvent.click(bar);
    await userEvent.click(screen.getByRole("button", { name: "Outside" }));
    expect(bar.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("progressbar")).toBeTruthy();
  });

  it("does not fabricate progress for a goal-only, empty or hidden card", () => {
    setShowsPlanProgress(true);
    const view = render(<PlanFold plan={[]} goal="Deliver a working feature" shown />);
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(screen.getByRole("button", { expanded: false }).textContent).toContain("Deliver a working feature");
    view.rerender(<PlanFold plan={[]} shown />);
    expect(view.container.innerHTML).toBe("");
    view.rerender(<PlanFold plan={plan} shown={false} />);
    expect(view.container.innerHTML).toBe("");
  });
});
