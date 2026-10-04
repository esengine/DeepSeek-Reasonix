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
  it("reads delivered once it has finished", () => {
    const html = draw("completed");
    expect(html).toContain('data-status="done"');
    expect(html).not.toContain("运行中");
  });
});
