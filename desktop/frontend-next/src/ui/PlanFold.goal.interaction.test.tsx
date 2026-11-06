// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PlanFold } from "./PlanFold";
import { boot, STORAGE } from "../i18n";
import type { GoalRuntime } from "../port/session";
import { workTime } from "./goalusage";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

const usage = (over: Partial<GoalRuntime> = {}): GoalRuntime => ({
  turnsUsed: 4, tokensUsed: 128_400, requestsUsed: 9, workDurationMs: 12 * 60_000, noProgressTurns: 0, ...over,
});

async function open(goalStatus: string, goalRuntime?: GoalRuntime) {
  render(<PlanFold plan={[]} goal="把发布说明写完" goalStatus={goalStatus} goalRuntime={goalRuntime} shown />);
  await userEvent.click(screen.getByRole("button", { expanded: false }));
}

describe("a goal's usage in the plan fold", () => {
  it("says what the goal has used, beside its state", async () => {
    await open("running", usage());
    const box = screen.getByRole("group", { name: "目标用量" });
    expect(document.querySelector(".goal-st")?.textContent).toBe("进行中 · 12m");
    expect(box.textContent).toBe("9 次请求 · 128.4K tokens · 用时 12m");
  });

  it("says why a stopped goal stopped, by its code, and the latest assessment", async () => {
    await open("blocked", usage({ stopCause: "goal_stuck", lastReason: "测试一直没有通过" }));
    const box = screen.getByRole("group", { name: "目标用量" });
    expect(within(box).getByText("目标没有进展")).toBeTruthy();
    expect(document.querySelector(".goal-st")?.getAttribute("data-tone")).toBe("warn");
    expect(within(box).getByText(/最近一次判断：测试一直没有通过/)).toBeTruthy();
  });

  it("does not repeat a stop cause on a goal that is running again", async () => {
    await open("running", usage({ stopCause: "goal_stuck" }));
    expect(screen.getByRole("group", { name: "目标用量" }).textContent).not.toContain("目标没有进展");
  });

  it("shows an unlisted stop cause by its code instead of guessing", async () => {
    await open("stopped", usage({ stopCause: "something_new" }));
    expect(screen.getByRole("group", { name: "目标用量" }).textContent).toContain("something_new");
  });

  it("draws nothing for usage when the kernel sends none", async () => {
    await open("running");
    expect(screen.queryByRole("group", { name: "目标用量" })).toBeNull();
    expect(screen.getByText("把发布说明写完", { selector: "p" })).toBeTruthy();
  });

  it("rounds work time the way the terminal and ACP do", () => {
    expect([0, 400, 1500, 59_400, 90_000, 3_600_000, 5_400_000].map(workTime)).toEqual(["0s", "1s", "2s", "59s", "2m", "1h", "1h 30m"]);
  });
});

describe("the goal's own controls", () => {
  it("pauses or ends a running goal, and resumes a stopped one", async () => {
    const sent: string[] = [];
    const { rerender } = render(<PlanFold plan={[]} goal="g" goalStatus="running" goalRuntime={usage()} shown onGoal={(c) => sent.push(c)} />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    await userEvent.click(screen.getByRole("button", { name: "暂停" }));
    await userEvent.click(screen.getByRole("button", { name: "退出目标" }));
    rerender(<PlanFold plan={[]} goal="g" goalStatus="stopped" goalRuntime={usage()} shown onGoal={(c) => sent.push(c)} />);
    await userEvent.click(screen.getByRole("button", { name: "继续" }));
    expect(sent).toEqual(["pause", "clear", "resume"]);
  });

  it("offers nothing on a goal that is done", async () => {
    render(<PlanFold plan={[]} goal="g" goalStatus="complete" goalRuntime={usage()} shown onGoal={() => {}} />);
    await userEvent.click(screen.getByRole("button", { expanded: false }));
    expect(screen.queryByRole("button", { name: "暂停" })).toBeNull();
  });
});
