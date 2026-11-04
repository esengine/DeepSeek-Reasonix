// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { DeckChips } from "./DeckChips";
import type { JobEntry } from "../port/port";
import type { Task } from "./panels/Agents";

afterEach(cleanup);

const job = (over: Partial<JobEntry> = {}): JobEntry =>
  ({ id: "j1", label: "pnpm dev", status: "running", startedAt: Date.now(), ...over }) as JobEntry;

const task = (running: boolean, id = "t1"): Task =>
  ({ t: "tool", id, running, children: [], tool: { name: "task", args: '{"subagent_type":"Explore"}' } }) as unknown as Task;

const draw = (tasks: Task[], jobs: JobEntry[]) =>
  render(<DeckChips tasks={tasks} jobs={jobs} open="" onOpen={vi.fn()} />).container;

// These are readings about the turn, so they live with the turn's other
// readings and open the way those do: the detail is part of the same object,
// not a panel somewhere else that a click has to go and find.
describe("the deck readings", () => {
  it("say nothing when nothing is running beside the conversation", () => {
    expect(draw([], []).querySelector(".studio-deck-anchor")).toBeNull();
  });

  it("carry their own detail rather than opening something elsewhere", () => {
    const box = draw([], [job()]);
    const anchor = box.querySelector(".studio-deck-anchor");
    expect(anchor?.querySelector('[data-action="deck.jobs"]')).not.toBeNull();
    // Inside the same anchor, which is what lets hover and focus reach it.
    expect(anchor?.querySelector(".studio-deck-pop")).not.toBeNull();
  });

  it("count only what is live, and read 0 when everything has ended", () => {
    expect(draw([], [job()]).querySelector('[data-action="deck.jobs"] b')?.textContent).toBe("1");
    const settled = draw([], [job({ status: "exited" })]);
    expect(settled.querySelector('[data-action="deck.jobs"] b')?.textContent).toBe("1");
    expect(settled.querySelector('[data-action="deck.jobs"] span')?.textContent).toBe("个后台任务 · 都结束了");
    expect(settled.querySelector('[data-action="deck.jobs"]')?.hasAttribute("data-live")).toBe(false);
  });

  it("reads how many jobs are running, then how many failed once none is", () => {
    const running = draw([], [job(), job({ id: "j2", status: "failed" })]);
    expect(running.querySelector('[data-action="deck.jobs"] span')?.textContent).toBe("个后台任务在跑");
    cleanup();
    const failed = draw([], [job({ status: "failed" }), job({ id: "j2", status: "failed" }), job({ id: "j3", status: "done" })]);
    const chip = failed.querySelector('[data-action="deck.jobs"]')!;
    expect(chip.querySelector("b")?.textContent).toBe("2");
    expect(chip.querySelector("span")?.textContent).toBe("个后台任务失败了");
    expect(chip.getAttribute("data-failed")).toBe("");
  });

  it("count sub-agents the same way: running ones on the chip, all of them in the popover", () => {
    const mix = [task(true, "a"), task(true, "b"), task(false, "c"), task(false, "d"), task(false, "e")];
    const box = draw(mix, []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("2");
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 5");
    expect(box.querySelectorAll(".studio-deck-pop .ag")).toHaveLength(5);
  });

  it("keeps the history in the popover when every sub-agent has finished", () => {
    const box = draw([task(false, "a"), task(false, "b")], []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("2");
    expect(box.querySelector('[data-action="deck.agents"] span')?.textContent).toBe("个子代理 · 都结束了");
    expect(box.querySelector('[data-action="deck.agents"]')?.hasAttribute("data-live")).toBe(false);
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 2");
  });

  it("does not count a failed or cancelled sub-agent as running", () => {
    const failed = { ...task(false, "f"), tool: { name: "task", args: "{}", err: "interrupted" } } as unknown as Task;
    const box = draw([failed, task(true, "g")], []);
    expect(box.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("1");
    expect(box.querySelector(".studio-deck-pop")?.textContent).toContain("共 2");
  });

  it("moves up by one when another sub-agent starts", () => {
    const first = [task(true, "a"), task(false, "b")];
    const view = render(<DeckChips tasks={first} jobs={[]} open="" onOpen={vi.fn()} />);
    expect(view.container.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("1");
    view.rerender(<DeckChips tasks={[...first, task(true, "c")]} jobs={[]} open="" onOpen={vi.fn()} />);
    expect(view.container.querySelector('[data-action="deck.agents"] b')?.textContent).toBe("2");
    expect(view.container.querySelector(".studio-deck-pop")?.textContent).toContain("共 3");
  });

  // Finished delegates stay reachable (their transcript opens from here), but
  // only a running one animates the reading.
  it("keeps finished delegates listed and animates only while one is running", () => {
    expect(draw([task(true)], []).querySelector('[data-action="deck.agents"]')?.hasAttribute("data-live")).toBe(true);
    const done = draw([task(false)], []).querySelector('[data-action="deck.agents"]');
    expect(done).not.toBeNull();
    expect(done?.hasAttribute("data-live")).toBe(false);
  });
});

describe("the sub-agent popover", () => {
  const named = (id: string, running: boolean, name: string, over: object = {}): Task =>
    ({ t: "tool", id, running, children: [], tool: { id: `call-${id}`, name: "task", profile: { name, label: `${name} task` }, ...over } }) as unknown as Task;

  it("titles the list, reads each delegate's state, and stops every running one", () => {
    const onStopCall = vi.fn();
    const box = render(<DeckChips tasks={[named("a", true, "test-writer"), named("b", false, "security-review", { durationMs: 8000 }), named("c", true, "doc-check")]} jobs={[]} open="agents" onOpen={vi.fn()} onStopCall={onStopCall} />).container;
    expect(box.querySelector(".agent-deck-head")?.textContent).toBe("本会话的子代理2 运行中 · 共 3");
    expect([...box.querySelectorAll(".agent-deck-state")].map((s) => s.textContent)).toEqual(["第 1 步", "已交付 · 8s", "第 1 步"]);
    (box.querySelector('[data-action="agent.stop-all"]') as HTMLButtonElement).click();
    expect(onStopCall.mock.calls.map((c) => c[0])).toEqual(["call-a", "call-c"]);
  });

  it("offers no stop once nothing is running", () => {
    const box = render(<DeckChips tasks={[named("b", false, "security-review")]} jobs={[]} open="agents" onOpen={vi.fn()} onStopCall={vi.fn()} />).container;
    expect(box.querySelector('[data-action="agent.stop-all"]')).toBeNull();
  });
});

describe("the background-job popover", () => {
  const ended = (over: Partial<JobEntry> = {}): JobEntry =>
    ({ id: "e1", kind: "bash", label: "go test ./...", description: "跑第 2 组内核测试", status: "failed", startedAt: 1_000, endedAt: 243_000, exitCode: 1, tail: "FAIL TestCompactKeepsPins (0.31s)", ...over }) as JobEntry;

  it("lists running jobs above ended ones, an ended one with its time, last line and exit status", () => {
    const box = render(<DeckChips tasks={[]} jobs={[job({ tail: "compiling module 192 … ok" }), ended()]} open="jobs" onOpen={vi.fn()} />).container;
    const heads = [...box.querySelectorAll(".job-deck-head b")].map((b) => b.textContent);
    expect(heads).toEqual(["在跑 · 1", "已结束 · 1"]);
    const rows = [...box.querySelectorAll(".job-deck-row")];
    expect(rows[0].querySelector(".job-deck-tail")?.textContent).toBe("compiling module 192 … ok");
    expect(rows[1].querySelector(".job-deck-time")?.textContent).toBe("4 分 02 秒 · 失败");
    expect(rows[1].querySelector(".job-deck-exit")?.textContent).toBe("退出 1");
    expect(rows[1].querySelector(".job-deck-tail")?.textContent).toBe("FAIL TestCompactKeepsPins (0.31s)");
  });

  it("clears the ended ones through the pane", () => {
    const onClearJobs = vi.fn();
    const box = render(<DeckChips tasks={[]} jobs={[job(), ended(), ended({ id: "e2", status: "done", exitCode: 0 })]} open="jobs" onOpen={vi.fn()} onClearJobs={onClearJobs} />).container;
    (box.querySelector('[data-action="job.clear"]') as HTMLButtonElement).click();
    expect(onClearJobs).toHaveBeenCalledWith(["e1", "e2"]);
  });

  it("stays red for a failure until the list has been opened on it", () => {
    const view = render(<DeckChips tasks={[]} jobs={[ended()]} open="" onOpen={vi.fn()} />);
    const chip = () => view.container.querySelector('[data-action="deck.jobs"]')!;
    expect(chip().hasAttribute("data-failed")).toBe(true);
    view.rerender(<DeckChips tasks={[]} jobs={[ended()]} open="jobs" onOpen={vi.fn()} />);
    view.rerender(<DeckChips tasks={[]} jobs={[ended()]} open="" onOpen={vi.fn()} />);
    expect(chip().hasAttribute("data-failed")).toBe(false);
    expect(chip().querySelector("span")?.textContent).toBe("个后台任务 · 都结束了");
  });
});
