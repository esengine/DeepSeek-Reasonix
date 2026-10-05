// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Runtime } from "./panels/Runtime";
import { Agents } from "./panels/Agents";
import { Jobs } from "./panels/Jobs";
import { Files } from "./panels/Files";
import type { Change, Stats } from "./panels/derive";
import type { Task } from "./panels/Agents";
import type { JobEntry } from "../port/port";

afterEach(cleanup);

const stats = (over: Partial<Stats> = {}): Stats =>
  ({ tools: 0, external: 0, failed: 0, waiting: 0, ...over });

// An ordinary run that is over and had nothing go wrong: the state the rail is
// in for most of its life.
const quiet = (over: Partial<Stats> = {}) =>
  render(<Runtime rate={0} done stats={stats(over)} />);

const task = (id: string, running: boolean): Task =>
  ({ t: "tool", id, running, children: [], tool: { id, name: "task", readOnly: true } } as Task);

describe("a runtime fact earns a row by having something to report", () => {
  // The defect: six counters standing at zero on every clean run, spending the
  // same width as they would on a broken one.
  it("draws no row of zeroes when nothing has happened", () => {
    const { container } = quiet();
    expect(container.textContent).toBe("");
    expect(container.querySelectorAll(".drow").length).toBe(0);
  });

  it("appears the moment a step fails, and says so as a fault", () => {
    expect(quiet().container.textContent).not.toMatch(/失败步数/);
    const { container } = quiet({ failed: 3 });
    const row = container.querySelector('.drow[data-tone="err"]');
    expect(row?.textContent).toContain("失败步数");
    expect(row?.textContent).toContain("3");
  });

  it("appears the moment something reaches outside", () => {
    expect(quiet().container.textContent).not.toMatch(/外部请求/);
    expect(quiet({ external: 2 }).container.textContent).toMatch(/外部请求/);
  });

  // Throughput is about a turn in flight. Once one is over it has nothing left
  // to say, and a permanent "— tok/s" is the same zero row by another spelling.
  it("keeps throughput to a turn that is running", () => {
    expect(quiet().container.textContent).not.toMatch(/tok\/s/);
    expect(render(<Runtime rate={0} done={false} stats={stats()} />).container.textContent)
      .toMatch(/tok\/s/);
  });
});

describe("what is waiting on the reader outranks what the run is doing", () => {
  // The one fact here somebody can act on now. The transcript says what is
  // being asked; this says that something is being asked at all.
  it("is absent at zero and present the moment something waits", () => {
    expect(quiet().container.querySelector('[data-tone="act"]')).toBeNull();
    const { container } = quiet({ waiting: 1 });
    expect(container.querySelector('[data-tone="act"]')?.textContent).toContain("待确认");
  });

  it("does not share the grammar of the counts beside it", () => {
    const { container } = quiet({ waiting: 1, failed: 2, external: 3 });
    const tones = [...container.querySelectorAll(".drow")].map((r) => r.getAttribute("data-tone"));
    expect(tones).toContain("act");
    // Whatever else is on the rail, nothing else claims the same weight.
    expect(tones.filter((x) => x === "act").length).toBe(1);
  });

  it("leads the rows, because it is what to do next rather than what happened", () => {
    const { container } = quiet({ waiting: 1, failed: 2, external: 3 });
    expect(container.querySelector(".drow")?.getAttribute("data-tone")).toBe("act");
  });
});

describe("the tool count is a fact, not a headline", () => {
  // D2 made it a correct cumulative fact. That is not the same as it deserving
  // a standing row: how many calls a turn made almost never decides what to do.
  it("is off the rows and onto the group, and only once there are any", () => {
    const { container } = quiet({ failed: 1, tools: 14 });
    expect([...container.querySelectorAll(".drow .k")].map((e) => e.textContent))
      .not.toContain("工具调用次数");
    expect(container.querySelector(".dgrp-h .a")?.textContent).toContain("14");
    expect(quiet({ failed: 1 }).container.querySelector(".dgrp-h .a")).toBeNull();
  });
});

describe("one fact, one place on the rail", () => {
  // 改动文件 and the工作树改动 block were the same derivation rendered twice,
  // four rows apart, from the same visible(changes, tree) call.
  it("does not draw the changed-file count a second time", () => {
    const { container } = quiet({ failed: 1 });
    expect(container.textContent).not.toMatch(/改动文件/);
  });
});

describe("panels with nothing to report do not hold a place", () => {
  it("says nothing about delegates a session never made", () => {
    expect(render(<Agents tasks={[]} />).container.textContent).toBe("");
    expect(render(<Agents tasks={[task("a", true)]} />).container.textContent).toMatch(/子代理/);
  });

  // Review posture puts this block first, so an unchanged tree made "nothing
  // happened" the first thing on the rail once a turn was over.
  it("does not lead the rail with a tree that has not changed", () => {
    const clean = render(<Files changes={[]} yolo={false} tree={null} />);
    expect(clean.container.textContent).toBe("");
    cleanup();
    const edits: Change[] = [{ path: "src/a.ts", added: 4, removed: 1, edits: 1 }];
    const dirty = render(<Files changes={edits} yolo={false} tree={null} />);
    expect(dirty.container.textContent).toMatch(/工作树改动/);
    expect(dirty.container.textContent).toMatch(/a\.ts/);
    expect(dirty.container.textContent).toMatch(/\+4/);
  });

  it("does not spend a block saying there are no background jobs", () => {
    expect(render(<Jobs jobs={[]} />).container.textContent).toBe("");
    const one = [{ id: "j1", status: "running", command: "npm test" }] as unknown as JobEntry[];
    expect(render(<Jobs jobs={one} />).container.textContent).toMatch(/后台任务/);
  });
});
