import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { initialState, reduce, type SessionEvent } from "../../state/session";
import { railOf } from "./derive";
import { Agents } from "./Agents";

const prof = { name: "flash-fill" };
const base: SessionEvent[] = [
  { kind: "tool_dispatch", tool: { id: "c1", name: "use_capability", profile: prof } },
  { kind: "tool_result", tool: { id: "c1", name: "use_capability", output: "Started background task", profile: prof } },
] as SessionEvent[];
const draw = (phase: string) => {
  const s = [...base, { kind: "tool_progress", tool: { id: "c1", name: "reasonix.subagent.status", output: phase } } as SessionEvent].reduce(reduce, initialState);
  return renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
};

describe("the Subagents panel for a background child", () => {
  it("reads running while the child works", () => {
    expect(draw("tool")).toContain('data-status="running"');
  });
  it("counts a queued child as running", () => {
    const html = draw("queued");
    expect(html).toContain('data-status="running"');
    expect(html).toContain("运行中 1 / 共 1");
  });
  it("reads delivered once it has finished", () => {
    const html = draw("completed");
    expect(html).toContain('data-status="done"');
    expect(html).not.toContain('data-status="running"');
  });
});

describe("a session restored from disk", () => {
  it("counts no restored sub-agent as running", () => {
    const s = [...base, { kind: "tool_progress", tool: { id: "c1", name: "reasonix.subagent.status", output: "completed" } } as SessionEvent].reduce(reduce, initialState);
    const html = renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
    expect(html).toContain("运行中 0 / 共 1");
  });
});

describe("a delegation the host refused before it ran", () => {
  const refused = [
    { kind: "tool_dispatch", tool: { id: "r1", name: "use_capability", profile: { name: "explore" } } },
    { kind: "tool_result", tool: { id: "r1", name: "use_capability", err: "invalid arguments for explore", refusalCode: "tool.arguments_invalid", profile: { name: "explore" } } },
  ] as SessionEvent[];
  const interrupted = [
    { kind: "tool_dispatch", tool: { id: "r2", name: "use_capability", profile: { name: "explore" } } },
    { kind: "tool_result", tool: { id: "r2", name: "use_capability", err: "sub-agent: context canceled", profile: { name: "explore" } } },
  ] as SessionEvent[];
  const html = (evs: SessionEvent[]) => {
    const s = evs.reduce(reduce, initialState);
    return renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
  };

  it("reads not run, never interrupted", () => {
    const out = html(refused);
    expect(out).toContain('data-status="refused"');
    expect(out).toContain("未执行");
    expect(out).not.toContain("已中断");
  });
  it("still reads interrupted for a call that started and was cut short", () => {
    const out = html(interrupted);
    expect(out).toContain('data-status="failed"');
    expect(out).toContain("已中断");
  });
});
