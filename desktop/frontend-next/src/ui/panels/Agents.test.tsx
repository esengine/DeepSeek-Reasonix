import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { initialState, reduce, type SessionEvent } from "../../state/session";
import { railOf } from "./derive";
import { Agents } from "./Agents";
import { seconds } from "../../i18n/format";

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
    expect(out).not.toContain("失败");
  });
  it("reads failed, with the reason on hover, for a call that started and was cut short", () => {
    const out = html(interrupted);
    expect(out).toContain('data-status="failed"');
    expect(out).toContain("失败");
    expect(out).toContain('title="');
  });
});

describe("telling parallel sub-agents apart", () => {
  const row = (name: string, label?: string) => [
    { kind: "tool_dispatch", tool: { id: name, name: "use_capability", profile: { name: "task", count: 1, label } } },
  ] as SessionEvent[];
  const html = (evs: SessionEvent[]) => {
    const s = evs.reduce(reduce, initialState);
    return renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
  };

  it("names each row by the kernel's task label, in full on hover", () => {
    const out = html([...row("a", "Audit the retry backoff"), ...row("b", "Check the docs build")]);
    expect(out).toContain('<span class="lbl" title="Audit the retry backoff">Audit the retry backoff</span>');
    expect(out).toContain('title="Check the docs build"');
  });

  it("draws no label where the kernel sent none, and none for a batch", () => {
    expect(html(row("a"))).not.toContain('class="lbl"');
    const fleet = [{ kind: "tool_dispatch", tool: { id: "f", name: "use_capability", profile: { name: "fleet", count: 3 } } }] as SessionEvent[];
    expect(html(fleet)).not.toContain('class="lbl"');
  });
});

describe("a delegate that has finished", () => {
  const finished = [
    { kind: "tool_dispatch", tool: { id: "d1", name: "use_capability", profile: { name: "explore" } } },
    { kind: "tool_result", tool: { id: "d1", name: "use_capability", output: "found it", durationMs: 1027000, profile: { name: "explore" } } },
  ] as SessionEvent[];
  const html = (evs: SessionEvent[]) => {
    const s = evs.reduce(reduce, initialState);
    return renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
  };

  it("says it is delivered and how long it took, not only the duration", () => {
    const out = html(finished);
    expect(out).toContain("已交付");
    expect(out).toContain(seconds(1027000, 0));
  });

  it("still says delivered when no duration was recorded", () => {
    const out = html([finished[0], { ...finished[1], tool: { ...(finished[1] as { tool: object }).tool, durationMs: 0 } } as SessionEvent]);
    expect(out).toContain("已交付");
  });
});

describe("the model a sub-agent ran on", () => {
  const ranOn = { kind: "tool_progress", tool: { id: "c1", name: "reasonix.subagent.status", output: "failed", profile: { model: "aigateway/model-x", effort: "high" } } } as SessionEvent;
  it("is drawn on the row of a failed run, with the full reference as its title", () => {
    const s = [...base, ranOn].reduce(reduce, initialState);
    const html = renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
    expect(html).toContain(">…/model-x<");
    expect(html).toContain('title="aigateway/model-x · high"');
  });
  it("is absent for a child whose model was never recorded", () => {
    expect(draw("running")).not.toContain('class="ran"');
  });
});

describe("two sub-agents whose model references differ only in the model name", () => {
  it("read differently in the row, however long the provider in front is", () => {
    const provider = "a-very-long-provider-name-that-fills-the-whole-row";
    const ran = (id: string, model: string) => [
      { kind: "tool_dispatch", tool: { id, name: "use_capability", profile: prof } },
      { kind: "tool_progress", tool: { id, name: "reasonix.subagent.status", output: "running", profile: { model: `${provider}/${model}`, effort: "high" } } },
    ] as SessionEvent[];
    const s = [...ran("c1", "deepseek-v4-pro"), ...ran("c2", "deepseek-v4-flash")].reduce(reduce, initialState);
    const html = renderToStaticMarkup(<Agents tasks={railOf(s.items, s.executions, s.subagentPhase).tasks} />);
    const chips = [...html.matchAll(/<span class="ran"[^>]*>([^<]*)<\/span>/g)].map((m) => m[1]);
    expect(chips).toEqual(["…/deepseek-v4-pro", "…/deepseek-v4-flash"]);
  });
});
