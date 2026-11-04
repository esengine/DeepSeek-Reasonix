// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { AgentRunView } from "./AgentRunView";
import { DeckChips } from "./DeckChips";
import type { AgentPort, HistoryMessage } from "../port/port";
import type { SubagentRun, SubagentTranscript } from "../port/subagent";
import type { Task } from "./panels/Agents";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const run = (over: Partial<SubagentRun> = {}): SubagentRun =>
  ({ ref: "sa_1", name: "audit", kind: "task", model: "deepseek/pro", state: "running", startedAt: 1, updatedAt: 2, ...over });

const say = (content: string, msgIndex: number): HistoryMessage => ({ role: "assistant", content, msgIndex });

function reader(pages: SubagentTranscript[]) {
  const calls: number[] = [];
  const port = {
    workspaceImageURL: () => "",
    subagentTranscript: vi.fn(async (_ref: string, after = 0) => {
      calls.push(after);
      return pages.find((p) => p.after === after) ?? pages[pages.length - 1];
    }),
  } as unknown as AgentPort;
  return { port, calls };
}

const page = (messages: HistoryMessage[], after: number, over: Partial<SubagentTranscript> = {}): SubagentTranscript => ({
  run: run(), messages, after, next: after + messages.length, total: after + messages.length, live: true, ...over,
});

describe("a sub-agent run read from the kernel's record", () => {
  it("shows what the run said and the state the kernel reports", async () => {
    const { port } = reader([page([say("reading the schema", 1)], 0, { live: false, run: run({ state: "completed" }) })]);
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    expect(await screen.findByText(/reading the schema/)).toBeTruthy();
    expect(screen.getByText("已完成")).toBeTruthy();
    expect(screen.getByText("deepseek/pro")).toBeTruthy();
  });

  it("keeps polling from where the last page ended while the run is live, and stops once it is not", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { port, calls } = reader([
      page([say("first", 1)], 0),
      page([say("second", 2)], 1),
      page([], 2, { live: false, run: run({ state: "completed" }) }),
    ]);
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    await screen.findByText(/first/);
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    await screen.findByText(/second/);
    expect(screen.getByText(/first/)).toBeTruthy();
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    const settled = calls.length;
    await act(async () => { await vi.advanceTimersByTimeAsync(6000); });
    expect(calls.slice(0, 3)).toEqual([0, 1, 2]);
    expect(calls.length).toBe(settled);
  });

  it("reads further pages straight away when the run holds more than one page", async () => {
    const { port, calls } = reader([
      page([say("one", 1)], 0, { total: 2 }),
      page([say("two", 2)], 1, { live: false, run: run({ state: "completed" }) }),
    ]);
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    await screen.findByText(/two/);
    expect(calls).toEqual([0, 1]);
  });

  it("says why it cannot be read instead of staying empty", async () => {
    const port = { workspaceImageURL: () => "", subagentTranscript: vi.fn(async () => { throw new Error("no such run"); }) } as unknown as AgentPort;
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    expect((await screen.findByRole("alert")).textContent).toContain("no such run");
  });

  it("reads nothing while its tab is not shown", () => {
    const { port } = reader([page([say("x", 1)], 0)]);
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown={false} />);
    expect(port.subagentTranscript).not.toHaveBeenCalled();
  });
});

const task = (id: string, call: string): Task =>
  ({ t: "tool", id, running: false, children: [], tool: { id: call, name: "task", args: '{"subagent_type":"Explore"}', profile: { name: "audit", count: 1 } } }) as unknown as Task;

describe("opening a sub-agent from the deck", () => {
  const open = async (onOpenRun: (call: string) => Promise<boolean>) => {
    render(<DeckChips tasks={[task("t1", "call_1")]} jobs={[]} open="agents" onOpen={vi.fn()} onOpenRun={onOpenRun} />);
    await act(async () => { fireEvent.click(document.querySelector('[data-action="agent.open"]')!); });
  };

  it("opens the run beside the conversation, by the call that spawned it, and keeps the dialog shut", async () => {
    const onOpenRun = vi.fn(async () => true);
    await open(onOpenRun);
    expect(onOpenRun).toHaveBeenCalledWith("call_1");
    expect(document.querySelector(".agent-tx")).toBeNull();
  });

  it("falls back to the full-record dialog when the call has no run to read", async () => {
    await open(vi.fn(async () => false));
    await waitFor(() => expect(document.querySelector(".agent-tx")).not.toBeNull());
  });

  it("falls back to the dialog when the window has no way to open a run", async () => {
    render(<DeckChips tasks={[task("t1", "call_1")]} jobs={[]} open="agents" onOpen={vi.fn()} />);
    await act(async () => { fireEvent.click(document.querySelector('[data-action="agent.open"]')!); });
    await waitFor(() => expect(document.querySelector(".agent-tx")).not.toBeNull());
  });
});

describe("the sub-agent tab's frame", () => {
  const asked = (content: string): HistoryMessage => ({ role: "user", content, msgIndex: 0 });

  it("puts the task the main session handed over above the run, once", async () => {
    const { port } = reader([page([asked("给 export 补测试，只改 tests/"), say("reading the schema", 1)], 0, { live: false, run: run({ state: "completed" }) })]);
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    await screen.findByText(/reading the schema/);
    expect(document.querySelector(".agentrun-task")?.textContent).toContain("给 export 补测试，只改 tests/");
    expect(screen.getAllByText(/给 export 补测试/)).toHaveLength(1);
  });

  it("stops the run through the call that spawned it while it runs, and only then", async () => {
    const { port } = reader([page([say("working", 1)], 0, { live: false, run: run({ parentCall: "call-9" }) })]);
    const stopToolCall = vi.fn(async () => {});
    Object.assign(port, { stopToolCall });
    const view = render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown />);
    fireEvent.click(await screen.findByRole("button", { name: "停止子代理" }));
    expect(stopToolCall).toHaveBeenCalledWith("call-9");
    view.unmount();
    const done = reader([page([say("done", 1)], 0, { live: false, run: run({ parentCall: "call-9", state: "completed" }) })]);
    render(<AgentRunView port={done.port} run={{ ref: "sa_1", name: "audit" }} shown />);
    await screen.findByText(/done/);
    expect(screen.queryByRole("button", { name: "停止子代理" })).toBeNull();
  });

  it("goes back to the group in the conversation by the spawning call", async () => {
    const { port } = reader([page([say("working", 1)], 0, { live: false, run: run({ parentCall: "call-9", state: "completed" }) })]);
    const onShowCall = vi.fn();
    render(<AgentRunView port={port} run={{ ref: "sa_1", name: "audit" }} shown onShowCall={onShowCall} />);
    fireEvent.click(await screen.findByRole("button", { name: "回到对话里那一组" }));
    expect(onShowCall).toHaveBeenCalledWith("call-9");
  });
});
