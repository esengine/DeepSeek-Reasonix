import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Graph } from "./Graph";
import { graphOf } from "../state/graph";
import { initialExecution, type ExecutionState } from "../state/execution";
import type { ExecutionGraph } from "../port/wire";

const fanOut: ExecutionGraph = {
  nodes: [
    { id: "g", kind: "group", state: "running", label: "fleet(2)" },
    { id: "g/1", parentId: "g", kind: "worker", state: "completed", label: "survey", profile: "explorer" },
    { id: "g/2", parentId: "g", kind: "worker", state: "queued", label: "audit", profile: "security-review", wait: "slots" },
  ],
  edges: [
    { from: "g", to: "g/1", kind: "spawn" },
    { from: "g", to: "g/2", kind: "spawn" },
  ],
};

const shown = (over: Partial<ExecutionState> = {}): string =>
  renderToStaticMarkup(
    <Graph run={{ ...initialExecution, phase: "live", graph: graphOf(fanOut), ...over }} items={[]} onOpen={() => {}} />,
  );

describe("what the run graph draws", () => {
  // Both nodes carry no model. One inherited the session's, the other was
  // opened before the worker layer recorded anything — and a view that drew
  // them alike would claim an observation the host never made.
  it("tells an unrecorded identity from an inherited one", () => {
    const inherited = shown();
    expect(inherited).not.toContain("身份未记录");

    const unrecorded = shown({ identityUnknown: ["g/2"] });
    expect(unrecorded).toContain("身份未记录");
    // Only the named one: the other's blank is still an inheritance.
    expect(unrecorded.match(/身份未记录/g)).toHaveLength(1);
  });

  it("names the executions nobody is running, and says they do not resume", () => {
    const html = shown({
      interruptions: [
        { execution: "g/1", kind: "interrupted-during-execution" },
        { execution: "g/2", kind: "interrupted-before-start" },
      ],
    });
    expect(html).toContain("2 项执行已经没有人在跑了");
    expect(html).toContain("已经开工，做到哪一步没有记录");
    expect(html).toContain("还没拿到空位，什么都没做");
    expect(html).toContain("不会自己接着跑");
  });

  // A run whose fan-out never formed still has interruptions to report, and the
  // "nothing was delegated" page would say the opposite.
  it("reports an interruption even when no lane survived it", () => {
    const html = renderToStaticMarkup(
      <Graph
        run={{
          ...initialExecution,
          phase: "live",
          interruptions: [{ execution: "x1", kind: "interrupted-before-start" }],
        }}
        items={[]}
        onOpen={() => {}}
      />,
    );
    expect(html).toContain("1 项执行已经没有人在跑了");
    expect(html).not.toContain("这一轮还没有派出子代理");
  });

  it("shows nothing of the last conversation while the next one is being read", () => {
    const html = shown({ phase: "loading" });
    expect(html).toContain("正在读取这一轮的运行图…");
    expect(html).not.toContain("survey");
  });
});
