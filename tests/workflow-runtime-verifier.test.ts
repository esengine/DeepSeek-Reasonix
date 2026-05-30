import { describe, expect, it } from "vitest";
import { runWorkflow } from "../src/workflow/runtime.js";
import type { WorkflowAgentRunner } from "../src/workflow/types.js";

describe("workflow verifier helpers", () => {
  it("verifyFindings fans out verifier agents and returns structured rows", async () => {
    const prompts: string[] = [];
    const runner: WorkflowAgentRunner = {
      run: async (prompt) => {
        prompts.push(prompt);
        return {
          ok: true,
          output: prompt.includes("first") ? "verified first" : "verified second",
        };
      },
    };

    const result = await runWorkflow(
      `export const meta = { name: "verify", description: "Verify findings" }
const rows = await verifyFindings([
  { id: "first", finding: "first risk" },
  { id: "second", finding: "second risk" },
])
return rows
`,
      { runner, maxAgents: 2, concurrency: 2 },
    );

    expect(result.success).toBe(true);
    expect(result.agentCount).toBe(2);
    expect(result.result).toEqual([
      { id: "first", finding: "first risk", verification: "verified first" },
      { id: "second", finding: "second risk", verification: "verified second" },
    ]);
    expect(prompts).toHaveLength(2);
  });

  it("synthesize runs a synthesis agent over compact inputs", async () => {
    const runner: WorkflowAgentRunner = {
      run: async (prompt, opts) => ({
        ok: opts.type === "synthesis" && prompt.includes("alpha"),
        output: "final synthesis",
      }),
    };

    const result = await runWorkflow(
      `export const meta = { name: "synth", description: "Synthesize" }
return await synthesize(["alpha", "beta"], { label: "final" })
`,
      { runner, maxAgents: 1 },
    );

    expect(result).toMatchObject({
      success: true,
      result: "final synthesis",
      agentCount: 1,
    });
  });
});
