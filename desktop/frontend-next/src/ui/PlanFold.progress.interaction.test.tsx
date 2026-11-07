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
    const toggle = within(settings.container).getByRole("switch");
    await userEvent.click(toggle);
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
    await userEvent.click(toggle);
    expect(screen.queryByRole("progressbar")).toBeNull();
    expect(localStorage.getItem("rx-plan-progress")).toBe("off");
  });

  it("follows live canonical statuses, including resetting the plan and completing all steps", () => {
    setShowsPlanProgress(true);
    const view = render(<PlanFold plan={plan} shown />);
    for (const status of ["pending", "in_progress", "completed"] as const) {
      view.rerender(<PlanFold plan={plan.map((s) => ({ ...s, status }))} shown />);
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
    expect([...document.querySelectorAll(".studio-todo-body .plan-step")].map((row) => row.getAttribute("data-state"))).toEqual(["done", "now", "todo"]);
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

  it("gives way to the plan's card on screen and to a decision waiting on a person, and closes when it does", async () => {
    setShowsPlanProgress(true);
    const view = render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    view.rerender(<PlanFold plan={plan} shown cardInView />);
    expect(view.container.innerHTML).toBe("");
    view.rerender(<PlanFold plan={plan} shown />);
    expect(screen.getByRole("button", { expanded: false })).toBeTruthy();
    view.rerender(<PlanFold plan={plan} shown paused />);
    expect(view.container.innerHTML).toBe("");
    view.rerender(<PlanFold plan={[]} goal="Deliver a working feature" shown paused cardInView />);
    expect(screen.getByRole("button", { expanded: false }).textContent).toContain("Deliver a working feature");
  });

  it("names the step in hand and spins only while the run moves", () => {
    const view = render(<PlanFold plan={plan} shown />);
    expect(view.container.querySelector(".studio-todo-sum b")?.textContent).toBe("计划 · 第 2 / 3 步");
    expect(view.container.querySelector(".studio-todo-sum .plan-spin")).toBeNull();
    view.rerender(<PlanFold plan={plan} shown running />);
    expect(view.container.querySelector(".studio-todo-sum .plan-spin")).not.toBeNull();
    view.rerender(<PlanFold plan={plan.map((s) => ({ ...s, status: "pending" as const }))} shown running />);
    expect(view.container.querySelector(".studio-todo-sum b")?.textContent).toBe("计划 · 第 1 / 3 步");
  });

  it("draws one segment per step in the strip, done, current and waiting", () => {
    setShowsPlanProgress(true);
    const { container } = render(<PlanFold plan={plan} shown />);
    const steps = [...container.querySelectorAll(".studio-todo-steps > i")].map((i) => i.getAttribute("data-state"));
    expect(steps).toEqual(["done", "current", "todo"]);
    expect(container.querySelector(".studio-todo-steps")?.getAttribute("aria-hidden")).toBe("true");
  });
});
