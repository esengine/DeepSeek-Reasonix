import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { PauseGate } from "../src/core/pause-gate.js";
import { ToolRegistry } from "../src/tools.js";
import { registerWorkflowTool } from "../src/tools/workflow.js";
import type {
  WorkflowAgentOptions,
  WorkflowAgentResult,
  WorkflowAgentRunner,
} from "../src/workflow/types.js";

class StaticRunner implements WorkflowAgentRunner {
  readonly calls: Array<{ prompt: string; opts: WorkflowAgentOptions }> = [];

  async run(prompt: string, opts: WorkflowAgentOptions): Promise<WorkflowAgentResult> {
    this.calls.push({ prompt, opts });
    return { ok: true, output: `ran:${opts.label ?? "agent"}` };
  }
}

const script = `export const meta = { name: "tool_workflow", description: "Tool workflow" }
const value = await agent("inspect", { label: "inspection" })
return { value }
`;

describe("registerWorkflowTool", () => {
  it("registers workflow", () => {
    const registry = new ToolRegistry();

    registerWorkflowTool(registry);

    expect(registry.has("workflow")).toBe(true);
  });

  it("validates scripts without running agents", async () => {
    const registry = new ToolRegistry();
    const runner = new StaticRunner();
    registerWorkflowTool(registry, { runner });

    const out = await registry.dispatch("workflow", { script, mode: "validate_only" });
    const parsed = JSON.parse(out);

    expect(parsed.success).toBe(true);
    expect(parsed.meta.name).toBe("tool_workflow");
    expect(parsed.agent_count).toBe(0);
    expect(runner.calls).toEqual([]);
  });

  it("dry-runs scripts without calling the configured runner", async () => {
    const registry = new ToolRegistry();
    const runner = new StaticRunner();
    registerWorkflowTool(registry, { runner });

    const out = await registry.dispatch("workflow", { script, mode: "dry_run" });
    const parsed = JSON.parse(out);

    expect(parsed.success).toBe(true);
    expect(parsed.agent_count).toBe(1);
    expect(parsed.planned_agents).toEqual([{ label: "inspection", promptPreview: "inspect" }]);
    expect(runner.calls).toEqual([]);
  });

  it("runs scripts through the configured runner", async () => {
    const registry = new ToolRegistry();
    const runner = new StaticRunner();
    registerWorkflowTool(registry, { runner });

    const out = await registry.dispatch("workflow", { script });
    const parsed = JSON.parse(out);

    expect(parsed.success).toBe(true);
    expect(parsed.result).toEqual({ value: "ran:inspection" });
    expect(parsed.agent_count).toBe(1);
    expect(runner.calls).toHaveLength(1);
  });

  it("starts background workflows through the manager", async () => {
    const registry = new ToolRegistry();
    const runner = new StaticRunner();
    const { WorkflowRunManager } = await import("../src/workflow/manager.js");
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    registerWorkflowTool(registry, { runner, manager });
    const gate = new PauseGate();
    gate.on((request) => gate.resolve(request.id, { type: "run_once" }));

    const out = await registry.dispatch(
      "workflow",
      { script, background: true },
      { confirmationGate: gate },
    );
    const parsed = JSON.parse(out);

    expect(parsed.success).toBe(true);
    expect(parsed.status).toBe("running");
    expect(parsed.run_id).toMatch(/^wf_/);

    const done = await manager.waitForRun(parsed.run_id);
    expect(done.status).toBe("completed");
    expect(done.result).toEqual({ value: "ran:inspection" });
  });

  it("rejects run mode scripts that never call agent", async () => {
    const registry = new ToolRegistry();
    registerWorkflowTool(registry, { runner: new StaticRunner() });

    const out = await registry.dispatch("workflow", {
      script: `export const meta = { name: "empty", description: "Empty" }\nreturn true`,
    });
    const parsed = JSON.parse(out);

    expect(parsed.success).toBe(false);
    expect(parsed.error).toMatch(/must call agent/i);
  });

  it("reports tool_mode full as mutating for plan-mode gating", async () => {
    const registry = new ToolRegistry();
    registerWorkflowTool(registry, { runner: new StaticRunner() });
    registry.setPlanMode(true);

    const out = await registry.dispatch("workflow", { script, tool_mode: "full" });
    const parsed = JSON.parse(out);

    expect(parsed.rejectedReason).toBe("plan-mode");
  });
});

describe("buildCodeToolset workflow registration", () => {
  let savedKey: string | undefined;
  let root: string;

  beforeEach(() => {
    savedKey = process.env.DEEPSEEK_API_KEY;
    // biome-ignore lint/performance/noDelete: absence is the behavior under test.
    delete process.env.DEEPSEEK_API_KEY;
    root = mkdtempSync(join(tmpdir(), "reasonix-workflow-toolset-"));
  });

  afterEach(() => {
    if (savedKey !== undefined) process.env.DEEPSEEK_API_KEY = savedKey;
    rmSync(root, { recursive: true, force: true });
  });

  it("builds code toolset with workflow without constructing a DeepSeek client", async () => {
    const { buildCodeToolset } = await import("../src/code/setup.js");

    const toolset = await buildCodeToolset({ rootDir: root });

    expect(toolset.tools.has("workflow")).toBe(true);
    await toolset.jobs.shutdown();
  });
});
