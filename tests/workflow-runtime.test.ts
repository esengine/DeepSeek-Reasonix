import { describe, expect, it } from "vitest";
import { runWorkflow } from "../src/workflow/runtime.js";
import type {
  WorkflowAgentOptions,
  WorkflowAgentResult,
  WorkflowAgentRunner,
} from "../src/workflow/types.js";

class RecordingRunner implements WorkflowAgentRunner {
  readonly calls: Array<{ prompt: string; opts: WorkflowAgentOptions }> = [];

  async run(prompt: string, opts: WorkflowAgentOptions): Promise<WorkflowAgentResult> {
    this.calls.push({ prompt, opts });
    const label = opts.label ?? "unlabeled";
    return { ok: true, output: `result:${label}` };
  }
}

const header = `export const meta = { name: "demo_workflow", description: "Demo workflow" }\n`;

describe("runWorkflow", () => {
  it("runs agent calls with phase and label metadata", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
phase("Scan")
const scan = await agent("inspect repository", { label: "repo scan", type: "explore" })
return { scan }
`,
      { runner },
    );

    expect(result.success).toBe(true);
    expect(result.agentCount).toBe(1);
    expect(result.result).toEqual({ scan: "result:repo scan" });
    expect(result.phases).toEqual(["Scan"]);
    expect(runner.calls[0]?.prompt).toBe("inspect repository");
    expect(runner.calls[0]?.opts).toMatchObject({
      label: "repo scan",
      phase: "Scan",
      type: "explore",
      workflowName: "demo_workflow",
    });
  });

  it("runs parallel thunks and preserves input order", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
const values = await parallel([
  () => agent("first", { label: "one" }),
  () => agent("second", { label: "two" }),
])
return values
`,
      { runner, concurrency: 2 },
    );

    expect(result.success).toBe(true);
    expect(result.result).toEqual(["result:one", "result:two"]);
    expect(runner.calls.map((call) => call.prompt)).toEqual(["first", "second"]);
  });

  it("starts parallel agent thunks concurrently", async () => {
    let active = 0;
    let maxActive = 0;
    const runner: WorkflowAgentRunner = {
      async run(_prompt, opts) {
        active++;
        maxActive = Math.max(maxActive, active);
        await new Promise((resolve) => setTimeout(resolve, 20));
        active--;
        return { ok: true, output: `result:${opts.label ?? "unlabeled"}` };
      },
    };

    const result = await runWorkflow(
      `${header}
const values = await parallel([
  () => agent("first", { label: "one" }),
  () => agent("second", { label: "two" }),
])
return values
`,
      { runner, concurrency: 2 },
    );

    expect(result.success).toBe(true);
    expect(result.result).toEqual(["result:one", "result:two"]);
    expect(maxActive).toBe(2);
  });

  it("accepts object-style agent input and maps instruction/name aliases", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
phase("Scan")
const scan = await agent({
  name: "repo scan",
  instruction: "inspect repository",
  type: "explore",
})
return { scan }
`,
      { runner },
    );

    expect(result.success).toBe(true);
    expect(result.result).toEqual({ scan: "result:repo scan" });
    expect(runner.calls[0]?.prompt).toBe("inspect repository");
    expect(runner.calls[0]?.opts).toMatchObject({
      label: "repo scan",
      phase: "Scan",
      type: "explore",
    });
  });

  it("rejects parallel inputs that are not thunks", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
return await parallel([agent("already started", { label: "bad" })])
`,
      { runner },
    );

    expect(result.success).toBe(false);
    expect(result.error).toMatch(/parallel\(\) expects an array of functions/);
  });

  it("runs pipeline stages sequentially per item", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
const values = await pipeline(
  ["a", "b"],
  async (item) => agent("stage1:" + item, { label: "stage1 " + item }),
  async (prev, item) => agent(prev + ":stage2:" + item, { label: "stage2 " + item }),
)
return values
`,
      { runner, concurrency: 2 },
    );

    expect(result.success).toBe(true);
    expect(result.result).toEqual(["result:stage2 a", "result:stage2 b"]);
    expect(runner.calls.map((call) => call.prompt)).toEqual([
      "stage1:a",
      "stage1:b",
      "result:stage1 a:stage2:a",
      "result:stage1 b:stage2:b",
    ]);
  });

  it("supports validate-only without executing the body", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
throw new Error("body should not run")
`,
      { runner, mode: "validate_only" },
    );

    expect(result.success).toBe(true);
    expect(result.agentCount).toBe(0);
    expect(runner.calls).toEqual([]);
  });

  it("supports dry-run without calling the runner", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
phase("Review")
const value = await agent("review auth", { label: "auth review" })
return { value }
`,
      { runner, mode: "dry_run" },
    );

    expect(result.success).toBe(true);
    expect(result.agentCount).toBe(1);
    expect(result.plannedAgents).toEqual([
      { label: "auth review", phase: "Review", promptPreview: "review auth" },
    ]);
    expect(runner.calls).toEqual([]);
  });

  it("enforces maxAgents before starting extra agents", async () => {
    const runner = new RecordingRunner();
    const result = await runWorkflow(
      `${header}
await agent("one", { label: "one" })
await agent("two", { label: "two" })
return true
`,
      { runner, maxAgents: 1 },
    );

    expect(result.success).toBe(false);
    expect(result.error).toMatch(/maxAgents/i);
    expect(runner.calls).toHaveLength(1);
  });
});
