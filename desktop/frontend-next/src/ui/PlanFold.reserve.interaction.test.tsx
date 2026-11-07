// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanFold } from "./PlanFold";
import { boot, STORAGE } from "../i18n";
import type { PlanStep } from "../state/session";

const plan: PlanStep[] = [
  { text: "确认插入位置与中文说明文案", status: "in_progress" },
  { text: "检查 git diff 仅含这一处改动", status: "pending" },
];

let height = 180;
let notify: (() => void) | null = null;

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.stubGlobal("ResizeObserver", class {
    constructor(cb: () => void) { notify = cb; }
    observe() { notify?.(); }
    disconnect() {}
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const h = this.classList.contains("studio-todo") ? height : 0;
    return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: h, width: 0, height: h, toJSON: () => ({}) };
  });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const composer = () => document.querySelector<HTMLElement>(".compose")!;

describe("the open plan above the composer", () => {
  it("reserves its own height on the composer so the transcript gives way to it", async () => {
    render(<div className="compose"><PlanFold plan={plan} shown /></div>);
    expect(composer().style.getPropertyValue("--todo-h")).toBe("");
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    expect(composer().style.getPropertyValue("--todo-h")).toBe("180px");
  });

  it("follows the list when it grows or shrinks", async () => {
    render(<div className="compose"><PlanFold plan={plan} shown /></div>);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    height = 260;
    notify?.();
    expect(composer().style.getPropertyValue("--todo-h")).toBe("260px");
    height = 180;
  });

  it("gives the space back when the list is put away", async () => {
    render(<div className="compose"><PlanFold plan={plan} shown /></div>);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    await userEvent.click(screen.getByRole("button", { name: "收起计划" }));
    expect(composer().style.getPropertyValue("--todo-h")).toBe("");
  });

  it("gives the space back when the strip goes away while open", async () => {
    const view = render(<div className="compose"><PlanFold plan={plan} shown /></div>);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    view.rerender(<div className="compose"><PlanFold plan={plan} shown={false} /></div>);
    expect(composer().style.getPropertyValue("--todo-h")).toBe("");
  });

  it("stands alone without a composer around it", async () => {
    render(<PlanFold plan={plan} shown />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    expect(screen.getByRole("button", { name: "收起计划" })).toBeTruthy();
  });
});
