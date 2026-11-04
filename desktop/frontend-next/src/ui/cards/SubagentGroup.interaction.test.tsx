// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import type { Item } from "../../state/session";
import { AgentRunOpener, SubagentGroup, agentHue, workLanes } from "./SubagentGroup";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(cleanup);

type Task = Extract<Item, { t: "tool" }>;
const agent = (id: string, name: string, label: string, over: Partial<Task["tool"]> = {}, running = false, steps = 0): Task => ({
  t: "tool", id, running,
  tool: { id: `call-${id}`, name: "task", readOnly: false, profile: { name, label }, ...over },
  children: Array.from({ length: steps }, (_, i) => ({ id: `${id}-c${i}`, name: "bash", readOnly: false, args: JSON.stringify({ command: `pytest tests/${i}` }), parentId: `call-${id}` })),
});
const read = (id: string): Item => ({ t: "tool", id, running: false, children: [], tool: { id, name: "read_file", readOnly: true, args: '{"path":"a.go"}' } });

describe("work with delegates sent out side by side", () => {
  it("splits two or more consecutive delegates into a group of their own, and leaves a lone one with the work", () => {
    const lanes = workLanes([read("r1"), agent("a", "x", "one"), agent("b", "y", "two"), read("r2")]);
    expect(lanes.map((l) => (l.agents ? `agents:${l.agents.length}` : `work:${l.items.length}`))).toEqual(["work:1", "agents:2", "work:1"]);
    const lone = workLanes([read("r1"), agent("a", "x", "one"), read("r2")]);
    expect(lone.map((l) => (l.agents ? "agents" : `work:${l.items.length}`))).toEqual(["work:3"]);
  });

  it("names each delegate, what it was sent to do, and where it stands", () => {
    render(<SubagentGroup tasks={[
      agent("a", "security-review", "检查令牌重用", { durationMs: 8000 }, false, 6),
      agent("b", "test-writer", "给 export 补测试", {}, true, 3),
      agent("c", "doc-check", "检查 README 示例", { err: "超出步数上限（30 步）", durationMs: 30000 }),
    ]} />);
    expect(screen.getByText("3 个子代理并行")).toBeTruthy();
    const rows = [...document.querySelectorAll(".agent-row")];
    expect(rows.map((r) => r.getAttribute("data-status"))).toEqual(["done", "running", "failed"]);
    expect(rows[0].querySelector(".agent-reading")?.textContent).toBe("已交付 · 6 步 · 8s");
    expect(rows[1].querySelector(".agent-reading")?.textContent).toContain("第 3 步");
    expect(rows[1].querySelector(".agent-row-note")?.textContent).toBe("正在运行命令 pytest tests/2");
    expect(rows[2].querySelector(".agent-row-note")?.textContent).toBe("超出步数上限（30 步）");
    expect(document.querySelector('.agent-pill[data-tone="run"]')?.textContent).toBe("1 运行中");
    expect(document.querySelector('.agent-pill[data-tone="err"]')?.textContent).toBe("1 失败");
  });

  it("stops one delegate or every running one through the port", async () => {
    const stopToolCall = vi.fn(async (_id: string) => {});
    render(<SubagentGroup port={{ stopToolCall }} tasks={[agent("a", "x", "one", {}, true, 1), agent("b", "y", "two", {}, true, 1), agent("c", "z", "three", { durationMs: 1000 })]} />);
    await userEvent.click(screen.getAllByRole("button", { name: "停止" })[0]);
    expect(stopToolCall).toHaveBeenLastCalledWith("call-a");
    await userEvent.click(screen.getByRole("button", { name: "全部停止" }));
    expect(stopToolCall.mock.calls.map((c) => c[0])).toEqual(["call-a", "call-a", "call-b"]);
  });

  it("folds to a summary once every delegate delivered, and stays open when one failed", async () => {
    const view = render(<SubagentGroup tasks={[agent("a", "x", "one", { durationMs: 8000 }), agent("b", "y", "two", { durationMs: 72000 })]} />);
    expect(document.querySelector(".agent-group-body")).toBeNull();
    expect(document.querySelector(".agent-group-sum")?.textContent).toBe("2 个都已交付 · 1m 12s");
    await userEvent.click(screen.getByRole("button", { name: "展开" }));
    expect(document.querySelectorAll(".agent-row")).toHaveLength(2);
    view.rerender(<SubagentGroup tasks={[agent("a", "x", "one"), agent("b", "y", "two", { err: "boom" })]} />);
    expect(document.querySelectorAll(".agent-row")).toHaveLength(2);
  });

  it("opens a delegate's run in the workbench when the pane can", async () => {
    const open = vi.fn(async () => true);
    render(<AgentRunOpener.Provider value={open}><SubagentGroup tasks={[agent("a", "x", "one", {}, true), agent("b", "y", "two", {}, true)]} /></AgentRunOpener.Provider>);
    await userEvent.click(screen.getAllByRole("button", { name: "在工作台打开" })[1]);
    expect(open).toHaveBeenCalledWith("call-b");
  });

  it("gives the same name the same colour wherever it is drawn", () => {
    expect(agentHue("test-writer")).toBe(agentHue("test-writer"));
    expect(new Set(["a", "b", "c", "d", "e", "f"].map(agentHue)).size).toBeGreaterThan(1);
  });
});
