import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import { type OrchestrateTaskResult, formatOrchestrationResult } from "../src/tools/orchestrate.js";
import type { SubagentResult } from "../src/tools/subagent.js";

function okResult(output: string, turns = 1, costUsd = 0.0003): SubagentResult {
  return {
    success: true,
    output,
    turns,
    toolIters: 1,
    elapsedMs: 100,
    costUsd,
    model: "deepseek-v4-flash",
    usage: new Usage(),
  };
}

function errResult(error: string): SubagentResult {
  return {
    success: false,
    output: "",
    error,
    turns: 0,
    toolIters: 0,
    elapsedMs: 50,
    costUsd: 0,
    model: "deepseek-v4-flash",
    usage: new Usage(),
  };
}

describe("formatOrchestrationResult", () => {
  it("returns 'No tasks provided.' for empty results", () => {
    expect(formatOrchestrationResult([])).toBe("No tasks provided.");
  });

  it("renders a summary table with all tasks succeeded", () => {
    const results: OrchestrateTaskResult[] = [
      { index: 0, task: "fix auth", result: okResult("done") },
      { index: 1, task: "add tests", result: okResult("ok") },
      { index: 2, task: "update docs", result: okResult("written") },
    ];
    const out = formatOrchestrationResult(results);
    expect(out).toContain("3/3 tasks completed");
    expect(out).toContain("| # | Task | Status | Turns | Cost |");
    expect(out).toContain("✅");
    expect(out).toContain("fix auth");
    expect(out).toContain("done");
  });

  it("marks failed tasks with ❌ and shows error", () => {
    const results: OrchestrateTaskResult[] = [
      { index: 0, task: "fix auth", result: okResult("done") },
      { index: 1, task: "bad task", result: errResult("something broke") },
    ];
    const out = formatOrchestrationResult(results);
    expect(out).toContain("1/2 tasks completed");
    expect(out).toContain("❌");
    expect(out).toContain("something broke");
  });

  it("truncates long task names in the table", () => {
    const longTask = "this is a very long task description that should be truncated in the table";
    const results: OrchestrateTaskResult[] = [{ index: 0, task: longTask, result: okResult("ok") }];
    const out = formatOrchestrationResult(results);
    // Task should be truncated to 40 chars + …
    expect(out).toContain("…");
  });

  it("includes total cost and turns", () => {
    const results: OrchestrateTaskResult[] = [
      { index: 0, task: "a", result: okResult("x", 3, 0.001) },
      { index: 1, task: "b", result: okResult("y", 5, 0.002) },
    ];
    const out = formatOrchestrationResult(results);
    expect(out).toContain("8"); // total turns = 3+5
    expect(out).toContain("0.0030"); // total cost = 0.001+0.002
  });
});
