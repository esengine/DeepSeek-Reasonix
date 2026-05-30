import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient, Usage } from "../src/client.js";
import { ToolRegistry } from "../src/tools.js";
import type { SpawnSubagentOptions, SubagentResult } from "../src/tools/subagent.js";
import { ReasonixWorkflowAgentRunner } from "../src/workflow/agent-runner.js";

function makeResult(output = "done"): SubagentResult {
  return {
    success: true,
    output,
    turns: 1,
    toolIters: 0,
    elapsedMs: 10,
    costUsd: 0,
    model: "deepseek-v4-flash",
    usage: new Usage(),
  };
}

describe("ReasonixWorkflowAgentRunner", () => {
  it("maps workflow agent calls to internal spawnSubagent options", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn() as unknown as typeof fetch,
    });
    const parentRegistry = new ToolRegistry();
    parentRegistry.register({ name: "read_file", readOnly: true, fn: () => "file" });
    const seen: SpawnSubagentOptions[] = [];
    const spawn = vi.fn(async (opts: SpawnSubagentOptions) => {
      seen.push(opts);
      return makeResult("agent output");
    });
    const signal = new AbortController().signal;
    const runner = new ReasonixWorkflowAgentRunner({ client, parentRegistry, spawn });

    const result = await runner.run("inspect repository", {
      workflowName: "repo_audit",
      label: "repo scan",
      phase: "Scan",
      type: "explore",
      signal,
    });

    expect(result).toMatchObject({ ok: true, output: "agent output" });
    expect(spawn).toHaveBeenCalledOnce();
    expect(seen[0]?.client).toBe(client);
    expect(seen[0]?.parentRegistry).toBe(parentRegistry);
    expect(seen[0]?.task).toBe("inspect repository");
    expect(seen[0]?.skillName).toBe("workflow:repo_audit:repo scan");
    expect(seen[0]?.parentSignal).toBe(signal);
    expect(seen[0]?.system).toContain("Workflow phase: Scan");
    expect(seen[0]?.system).toContain("Workflow agent type: explore");
  });

  it("defaults read-only workflows to registered safe tools only", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn() as unknown as typeof fetch,
    });
    const parentRegistry = new ToolRegistry();
    parentRegistry.register({ name: "read_file", readOnly: true, fn: () => "file" });
    parentRegistry.register({ name: "search_content", readOnly: true, fn: () => "matches" });
    parentRegistry.register({ name: "write_file", fn: () => "write" });
    parentRegistry.register({ name: "workflow", readOnly: true, fn: () => "workflow" });
    parentRegistry.register({ name: "spawn_subagent", readOnly: true, fn: () => "spawn" });
    parentRegistry.register({ name: "submit_plan", readOnly: true, fn: () => "plan" });
    const seen: SpawnSubagentOptions[] = [];
    const spawn = vi.fn(async (opts: SpawnSubagentOptions) => {
      seen.push(opts);
      return makeResult();
    });
    const runner = new ReasonixWorkflowAgentRunner({ client, parentRegistry, spawn });

    await runner.run("read only", { workflowName: "wf" });

    expect(seen[0]?.allowedTools).toEqual(["read_file", "search_content"]);
  });
});
