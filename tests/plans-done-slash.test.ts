import { describe, expect, it, vi } from "vitest";
import { handleSlash } from "../src/cli/ui/slash/dispatch.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../src/index.js";
import { ToolRegistry } from "../src/tools.js";

function makeLoop(sessionName: string | null = "test-session"): CacheFirstLoop {
  const loop = new CacheFirstLoop({
    client: new DeepSeekClient({ apiKey: "sk-test" }),
    prefix: new ImmutablePrefix({ system: "s", toolSpecs: [] }),
    tools: new ToolRegistry(),
    maxToolIters: 1,
    stream: false,
  });
  Object.defineProperty(loop, "sessionName", { value: sessionName, writable: true });
  return loop;
}

describe("/plans done slash handler (issue #641)", () => {
  it("rejects when no subcommand arg is given (`/plans done`)", () => {
    const result = handleSlash("plans", ["done"], makeLoop(), {});
    expect(result.info).toMatch(/usage: \/plans done/);
  });

  it("falls through to the default listing when no `done` subcommand is given", () => {
    const result = handleSlash("plans", [], makeLoop(), {});
    expect(result.info).toMatch(/active plan/i);
  });

  it("forwards a single-step done to ctx.markPlanStepDone and surfaces ok", () => {
    const markPlanStepDone = vi.fn().mockReturnValue("ok");
    const result = handleSlash("plans", ["done", "step-3"], makeLoop(), { markPlanStepDone });
    expect(markPlanStepDone).toHaveBeenCalledWith("step-3");
    expect(result.info).toMatch(/marked step .*step-3.* done/);
  });

  it("maps not-in-plan / already-done / no-plan outcomes to distinct messages", () => {
    const cases = [
      { outcome: "not-in-plan", expected: /not in the active plan/i },
      { outcome: "already-done", expected: /already marked done/i },
      { outcome: "no-plan", expected: /no active plan/i },
    ] as const;
    for (const c of cases) {
      const fn = vi.fn().mockReturnValue(c.outcome);
      const result = handleSlash("plans", ["done", "step-1"], makeLoop(), { markPlanStepDone: fn });
      expect(result.info).toMatch(c.expected);
    }
  });

  it("`/plans done all` calls markAllPlanStepsDone and reports the count", () => {
    const markAllPlanStepsDone = vi.fn().mockReturnValue(7);
    const result = handleSlash("plans", ["done", "all"], makeLoop(), { markAllPlanStepsDone });
    expect(markAllPlanStepsDone).toHaveBeenCalledOnce();
    expect(result.info).toMatch(/marked 7 step/);
  });

  it("`/plans done all` reports the no-op case when nothing remained", () => {
    const markAllPlanStepsDone = vi.fn().mockReturnValue(0);
    const result = handleSlash("plans", ["done", "all"], makeLoop(), { markAllPlanStepsDone });
    expect(result.info).toMatch(/already done/i);
  });

  it("reports unavailable when the callbacks aren't wired (e.g. tests context)", () => {
    const result = handleSlash("plans", ["done", "step-1"], makeLoop(), {});
    expect(result.info).toMatch(/only available inside an active session/i);
    const all = handleSlash("plans", ["done", "all"], makeLoop(), {});
    expect(all.info).toMatch(/only available inside an active session/i);
  });
});
