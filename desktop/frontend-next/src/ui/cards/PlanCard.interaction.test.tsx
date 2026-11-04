// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { ApprovalCard } from "./ApprovalCard";
import { PlanLiveContext, type PlanLive } from "../planLive";
import type { Item, PlanStep } from "../../state/session";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

type Gate = Extract<Item, { t: "approval" }>;

const todos = [
  { content: "读出导出脚本", status: "in_progress" },
  { content: "改成按 GBK 解码", status: "pending" },
  { content: "跑全量测试", status: "pending" },
];
const gate = (over: Partial<Gate> = {}): Gate => ({
  t: "approval", id: "g1", a: { id: "a1", tool: "exit_plan_mode", subject: "", kind: "plan", input: { todos } }, ...over,
});
const steps = (...status: PlanStep["status"][]): PlanStep[] => todos.map((x, i) => ({ text: x.content, status: status[i] }));
const live = (over: Partial<PlanLive> = {}): PlanLive => ({ plan: [], running: false, blocked: false, latest: "g1", onSeen: vi.fn(), ...over });

function show(item: Gate, value: PlanLive | null, onPlan = vi.fn(async () => {})) {
  const tree = (it: Gate, v: PlanLive | null) => (
    <PlanLiveContext.Provider value={v}>
      <ApprovalCard item={it} onApprove={vi.fn()} onFullAccess={vi.fn()} onPlan={onPlan} />
    </PlanLiveContext.Provider>
  );
  const view = render(tree(item, value));
  return { ...view, onPlan, again: (it: Gate, v: PlanLive | null) => view.rerender(tree(it, v)) };
}

const card = () => document.querySelector(".plan-card")!;
const badge = () => card().querySelector(".plan-card-badge")?.textContent;
const marks = () => [...card().querySelectorAll(".plan-step")].map((row) => row.getAttribute("data-state"));

describe("the plan card", () => {
  it("lists the proposed steps by number before anything runs, with the three answers the kernel tells apart", async () => {
    const { onPlan } = show(gate(), live({ blocked: true }));
    expect(badge()).toBe("待确认");
    expect(marks()).toEqual(["todo", "todo", "todo"]);
    expect([...card().querySelectorAll(".plan-step-mark")].map((m) => m.textContent)).toEqual(["1", "2", "3"]);
    expect(card().textContent).toContain("改成按 GBK 解码");
    expect([...card().querySelectorAll('[data-action="decision.plan"]')].map((b) => b.getAttribute("data-value"))).toEqual(["start", "revise", "exit"]);
    await userEvent.click(screen.getByRole("button", { name: "开始执行" }));
    expect(onPlan).toHaveBeenCalledWith("g1", "a1", "start");
  });

  it("follows the live list in place once started, and says which step it is on", () => {
    show(gate({ verdict: "start" }), live({ plan: steps("completed", "in_progress", "pending"), running: true }));
    expect(card().getAttribute("data-phase")).toBe("running");
    expect(badge()).toBe("第 2 / 3 步");
    expect(card().querySelector(".plan-card-sub")?.textContent).toBe("正在做：改成按 GBK 解码");
    expect(marks()).toEqual(["done", "now", "todo"]);
    expect(card().querySelector(".plan-step[data-state=now] .plan-spin")).not.toBeNull();
    expect(card().querySelector(".plan-card-foot")).toBeNull();
  });

  it("marks the card waiting while a person is asked, without the turning mark", () => {
    show(gate({ verdict: "start" }), live({ plan: steps("completed", "in_progress", "pending"), running: true, blocked: true }));
    expect(card().getAttribute("data-phase")).toBe("waiting");
    expect(badge()).toBe("等你定");
    expect(card().querySelector(".plan-card-sub")?.textContent).toBe("停在第 2 步：改成按 GBK 解码");
    expect(card().querySelector(".plan-spin")).toBeNull();
  });

  it("keeps the finished list after the pane lets a spent plan go", () => {
    const view = show(gate({ verdict: "start" }), live({ plan: steps("completed", "completed", "completed"), running: true }));
    expect(badge()).toBe("做完了");
    view.again(gate({ verdict: "start", steps: steps("completed", "completed", "completed") }), live({ plan: [], running: false }));
    expect(badge()).toBe("做完了");
    expect(card().querySelector(".plan-card-sub")?.textContent).toBe("3 步全部完成");
    expect(marks()).toEqual(["done", "done", "done"]);
  });

  it("leaves an older plan's card alone when a newer plan holds the live list", () => {
    show(gate({ verdict: "start" }), live({ latest: "g2", plan: steps("in_progress", "pending", "pending"), running: true }));
    expect(card().getAttribute("data-phase")).toBe("stopped");
    expect(badge()).toBe("已停下");
    expect(card().querySelector(".plan-spin")).toBeNull();
  });

  it("names a declined or revised plan and offers nothing more to press", () => {
    const view = show(gate({ verdict: "revise" }), live());
    expect(badge()).toBe("继续规划");
    expect(marks()).toEqual(["todo", "todo", "todo"]);
    expect(card().querySelector(".plan-card-foot")).toBeNull();
    view.again(gate({ verdict: "exit" }), live());
    expect(badge()).toBe("暂不执行");
  });

  it("says when the current plan's card is on screen, and stops saying so when it goes", () => {
    const onSeen = vi.fn();
    const view = show(gate({ verdict: "start" }), live({ plan: steps("in_progress", "pending", "pending"), running: true, onSeen }));
    expect(onSeen).toHaveBeenLastCalledWith(true);
    view.unmount();
    expect(onSeen).toHaveBeenLastCalledWith(false);
  });

  it("does not watch a card that is still a proposal", () => {
    const onSeen = vi.fn();
    show(gate(), live({ onSeen, blocked: true }));
    expect(onSeen).not.toHaveBeenCalled();
  });
});
