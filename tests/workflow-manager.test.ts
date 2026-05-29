import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import { WorkflowRunManager } from "../src/workflow/manager.js";
import type { WorkflowAgentRunner } from "../src/workflow/types.js";

const script = `export const meta = { name: "parallel_smoke", description: "Parallel smoke" }
phase("Review")
const result = await parallel([
  () => agent("alpha", { label: "alpha" }),
  () => agent("beta", { label: "beta" }),
])
return result
`;

describe("WorkflowRunManager", () => {
  it("starts a background run and records completion", async () => {
    const runner: WorkflowAgentRunner = {
      async run(_prompt, opts) {
        await new Promise((resolve) => setTimeout(resolve, 5));
        return { ok: true, output: `ok:${opts.label}` };
      },
    };
    const events: string[] = [];
    const manager = new WorkflowRunManager({
      runner,
      rootDir: process.cwd(),
      onEvent: (event) => events.push(event.type),
    });

    const started = manager.startRun({ script, mode: "run", concurrency: 2, maxAgents: 2 });
    expect(started.status).toBe("running");
    expect(started.name).toBe("parallel_smoke");
    expect(manager.listRuns()).toHaveLength(1);

    const done = await manager.waitForRun(started.id);
    expect(done.status).toBe("completed");
    expect(done.result).toEqual(["ok:alpha", "ok:beta"]);
    expect(done.agentCount).toBe(2);
    expect(events).toContain("workflow.started");
    expect(events).toContain("workflow.agent.started");
    expect(events).toContain("workflow.completed");
  });

  it("stops a running workflow", async () => {
    const runner: WorkflowAgentRunner = {
      async run(_prompt, opts) {
        await new Promise((_resolve, reject) => {
          opts.signal?.addEventListener("abort", () => reject(new Error("aborted")), {
            once: true,
          });
        });
        return { ok: true, output: "unreachable" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run", concurrency: 2, maxAgents: 2 });

    const stopped = manager.stopRun(started.id);
    expect(stopped.status).toBe("aborted");

    const done = await manager.waitForRun(started.id);
    expect(done.status).toBe("aborted");
    expect(done.error).toMatch(/aborted/i);
  });

  it("aggregates usage and cost from raw subagent results when available", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return {
          ok: true,
          output: "ok",
          raw: {
            success: true,
            output: "ok",
            turns: 1,
            toolIters: 0,
            elapsedMs: 1,
            costUsd: 0.001,
            model: "test-model",
            usage: new Usage(10, 5, 15, 8, 2),
          },
        };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });

    const done = await manager.waitForRun(started.id);

    expect(done.usage?.promptTokens).toBe(20);
    expect(done.usage?.completionTokens).toBe(10);
    expect(done.usage?.totalTokens).toBe(30);
    expect(done.usage?.promptCacheHitTokens).toBe(16);
    expect(done.usage?.promptCacheMissTokens).toBe(4);
    expect(done.costUsd).toBe(0.002);
  });
});
