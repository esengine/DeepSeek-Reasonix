// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import "./testkit";
import { ActivityHead, Bump, groupSpan, staggered } from "./ActivityHead";
import { stepSummary } from "./cards/stepsum";
import type { Item } from "../state/session";

afterEach(cleanup);

type Tool = Extract<Item, { t: "tool" }>;
const call = (id: string, name: string, args: string, over: Partial<Tool["tool"]> = {}, running = false): Tool => ({
  t: "tool", id, running, children: [], tool: { id, name, args, readOnly: true, ...over },
});
const head = (items: Item[], open: boolean, runs: number, failures = 0) =>
  render(<details open={open}><ActivityHead items={items} open={open} runs={runs} failures={failures} calls={items.length} /></details>);

describe("the activity group's title, as the Think-Exec board draws it", () => {
  it("says what it is doing now while it runs folded", () => {
    head([call("r", "read_file", '{"path":"a.go"}'), call("b", "bash", '{"command":"go test ./..."}', {}, true)], false, 1);
    expect(document.querySelector(".activity-now")!.textContent).toBe("运行命令 go test ./... · 0 秒");
    expect(document.querySelector(".activity-now .step-spin")).not.toBeNull();
    expect(document.querySelector(".activity-running")).toBeNull();
  });

  it("counts what runs once it is open", () => {
    head([call("b", "bash", '{"command":"go test"}', {}, true)], true, 1);
    expect(document.querySelector(".activity-running")!.textContent).toBe("1 项运行中");
    expect(document.querySelector(".activity-now")).toBeNull();
  });

  it("says what it came to once done: a tick, the count and how long it took", () => {
    head([call("r", "read_file", "{}", { startedAt: 1_000, endedAt: 2_000 }), call("b", "bash", "{}", { startedAt: 3_000, endedAt: 39_000 })], false, 0);
    expect(document.querySelector(".activity-ok")).not.toBeNull();
    expect(document.querySelector(".activity-count")!.textContent).toBe("2 项操作 · 38 秒");
  });

  it("keeps the failure on the title and claims no tick", () => {
    head([call("b", "bash", "{}", { err: "x" })], true, 0, 1);
    expect(document.querySelector(".activity-errors")!.textContent).toBe("1 项失败");
    expect(document.querySelector(".activity-ok")).toBeNull();
  });
});

describe("the parts that move", () => {
  it("rolls a count that changes under the reader, and not one first drawn", () => {
    const view = render(<Bump value={3}>3 项操作</Bump>);
    expect(document.querySelector("[data-moved]")).toBeNull();
    act(() => view.rerender(<Bump value={4}>4 项操作</Bump>));
    expect(document.querySelector("[data-moved]")!.textContent).toBe("4 项操作");
  });

  it("staggers the rows entering together, at most five apart", () => {
    const rows = Array.from({ length: 8 }, (_, i) => call(`c${i}`, "read_file", "{}"));
    const owed = new Set(rows.slice(1).map((r) => r.id));
    expect(staggered(rows, owed).map(([, k]) => k)).toEqual([0, 0, 1, 2, 3, 4, 5, 5]);
  });

  it("measures the group from the first start to the last end, else adds the durations", () => {
    expect(groupSpan([call("a", "x", "{}", { startedAt: 10, endedAt: 50 }), call("b", "x", "{}", { startedAt: 20, endedAt: 90 })])).toBe(80);
    expect(groupSpan([call("a", "x", "{}", { durationMs: 300 }), call("b", "x", "{}", { durationMs: 200 })])).toBe(500);
  });
});

describe("what a finished step came to", () => {
  it("reads lines, matches and a failed command's exit status from the call itself", () => {
    expect(stepSummary(call("r", "read_file", "{}", { output: "a\nb\nc\n" }).tool, "chat.read", false)).toBe("3 行");
    expect(stepSummary(call("s", "grep", "{}", { output: "a:1\n\nb:2" }).tool, "chat.search", false)).toBe("2 处");
    expect(stepSummary(call("b", "bash", "{}", { execution: { exitCode: 1 } }).tool, "chat.run_command", true)).toBe("退出 1");
    expect(stepSummary(call("b", "bash", "{}", { output: "ok" }).tool, "chat.run_command", false)).toBe("");
  });
});
