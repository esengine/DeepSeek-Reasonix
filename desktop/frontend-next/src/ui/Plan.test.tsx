import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Plan } from "./Plan";
import type { PlanStep } from "../state/session";

const steps = (...done: boolean[]): PlanStep[] =>
  done.map((d, i) => ({
    text: `第 ${i + 1} 步`,
    status: d ? "completed" : i === done.indexOf(false) ? "in_progress" : "pending",
  }));

describe("the plan block", () => {
  // No plan and an empty plan are the same absence. A block reading "0 / 0 ·
  // 尚未制定" is a wall to skip on every glance at a rail that already has nine.
  it("draws nothing when there is no plan", () => {
    expect(renderToStaticMarkup(<Plan steps={[]} />)).toBe("");
  });

  it("counts what is done against what there is", () => {
    const html = renderToStaticMarkup(<Plan steps={steps(true, true, false, false)} />);
    expect(html).toContain('data-b="plan"');
    expect(html).toContain("2 / 4");
    expect(html).toContain("width:50%");
  });

  it("marks exactly one step as the one happening now", () => {
    const html = renderToStaticMarkup(<Plan steps={steps(true, false, false)} />);
    expect(html.match(/data-now=""/g)).toHaveLength(1);
    expect(html.match(/data-done=""/g)).toHaveLength(1);
  });

  // Every step done means nothing is happening now — not that the last one is.
  it("points at no step once they are all done", () => {
    expect(renderToStaticMarkup(<Plan steps={steps(true, true)} />)).not.toContain('data-now');
  });

  it("points at no step while the host has started none", () => {
    const written: PlanStep[] = [
      { text: "一", status: "pending" },
      { text: "二", status: "pending" },
    ];
    expect(renderToStaticMarkup(<Plan steps={written} />)).not.toContain("data-now");
  });

  it("points where the host points, not at the first unticked step", () => {
    const skipped: PlanStep[] = [
      { text: "一", status: "pending" },
      { text: "二", status: "in_progress", activeForm: "正在做第二步" },
      { text: "三", status: "pending" },
    ];
    const rows = renderToStaticMarkup(<Plan steps={skipped} />).split('class="s"').slice(1);
    expect(rows.map((r) => r.includes('data-now=""'))).toEqual([false, true, false]);
    expect(rows[1]).toContain("正在做第二步");
  });

  it("uses the step's own text once it is no longer the one underway", () => {
    const settled: PlanStep[] = [{ text: "写测试", status: "completed", activeForm: "正在写测试" }];
    const html = renderToStaticMarkup(<Plan steps={settled} />);
    expect(html).toContain("写测试");
    expect(html).not.toContain("正在写测试");
  });

  it("indents a sub-step by its level", () => {
    const nested: PlanStep[] = [
      { text: "父", status: "in_progress" },
      { text: "子", status: "pending", level: 1 },
    ];
    expect(renderToStaticMarkup(<Plan steps={nested} />)).toContain("margin-inline-start:12px");
  });

  // todo_write rewrites the whole list every call, so two steps with the same
  // words must still be two rows rather than one React key collision.
  it("keeps duplicate steps apart", () => {
    const same: PlanStep[] = [{ text: "跑测试", status: "completed" }, { text: "跑测试", status: "in_progress" }];
    expect(renderToStaticMarkup(<Plan steps={same} />).match(/class="s"/g)).toHaveLength(2);
  });
});
