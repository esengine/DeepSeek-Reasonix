import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { handleWorkflowsSlash } from "../src/cli/ui/slash/handlers/workflows.js";
import { WorkflowRunManager } from "../src/workflow/manager.js";
import type { WorkflowAgentRunner } from "../src/workflow/types.js";

const script = `export const meta = { name: "audit", description: "Audit" }
const value = await agent("inspect", { label: "inspection" })
return value
`;

describe("/workflows slash handler", () => {
  it("lists workflow runs", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "ok" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });
    await manager.waitForRun(started.id);

    const result = handleWorkflowsSlash([], { workflowManager: manager });

    expect(result.info).toContain("audit");
    expect(result.info).toContain("completed");
  });

  it("shows run details", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "ok" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });
    await manager.waitForRun(started.id);

    const result = handleWorkflowsSlash(["show", started.id], { workflowManager: manager });

    expect(result.info).toContain("inspection");
    expect(result.info).toContain("ok");
  });

  it("reports missing manager clearly", () => {
    const result = handleWorkflowsSlash([], {});

    expect(result.info).toMatch(/not available/i);
  });

  it("runs a saved workflow by name", async () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-workflows-slash-"));
    const home = mkdtempSync(join(tmpdir(), "reasonix-workflows-home-"));
    try {
      mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
      writeFileSync(join(root, ".reasonix", "workflows", "audit.js"), script, "utf8");
      const runner: WorkflowAgentRunner = {
        async run(prompt) {
          return { ok: true, output: `ok:${prompt}` };
        },
      };
      const manager = new WorkflowRunManager({ runner, rootDir: root, homeDir: home });

      const result = handleWorkflowsSlash(["run", "audit", "extra", "input"], {
        workflowManager: manager,
        codeRoot: root,
        homeDir: home,
      });
      const runId = result.info?.match(/wf_[a-z0-9]+/)?.[0];

      expect(runId).toBeDefined();
      const done = await manager.waitForRun(runId!);
      expect(done.status).toBe("completed");
      expect(done.result).toBe("ok:inspect");
    } finally {
      rmSync(root, { recursive: true, force: true });
      rmSync(home, { recursive: true, force: true });
    }
  });

  it("injects the workflow runner when running a saved workflow", async () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-workflows-slash-runner-"));
    const home = mkdtempSync(join(tmpdir(), "reasonix-workflows-home-runner-"));
    try {
      mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
      writeFileSync(join(root, ".reasonix", "workflows", "audit.js"), script, "utf8");
      const runner: WorkflowAgentRunner = {
        async run(prompt) {
          return { ok: true, output: `runner:${prompt}` };
        },
      };
      const manager = new WorkflowRunManager({ rootDir: root, homeDir: home });

      const result = handleWorkflowsSlash(["run", "audit"], {
        workflowManager: manager,
        workflowRunner: runner,
        codeRoot: root,
        homeDir: home,
      });
      const runId = result.info?.match(/wf_[a-z0-9]+/)?.[0];

      expect(runId).toBeDefined();
      const done = await manager.waitForRun(runId!);
      expect(done.result).toBe("runner:inspect");
    } finally {
      rmSync(root, { recursive: true, force: true });
      rmSync(home, { recursive: true, force: true });
    }
  });

  it("retries a completed run with the original script", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "ok" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const first = manager.startRun({ script, mode: "run", maxAgents: 1 });
    await manager.waitForRun(first.id);

    const result = handleWorkflowsSlash(["retry", first.id], { workflowManager: manager });

    expect(result.info).toMatch(/started retry/);
    expect(manager.listRuns()).toHaveLength(2);
  });

  it("injects the workflow runner when retrying a run", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "retry-ok" };
      },
    };
    const manager = new WorkflowRunManager({ rootDir: process.cwd() });
    const first = manager.startRun({ script, mode: "run", maxAgents: 1, runner });
    await manager.waitForRun(first.id);

    const result = handleWorkflowsSlash(["retry", first.id], {
      workflowManager: manager,
      workflowRunner: runner,
    });
    const runId = result.info?.match(/wf_[a-z0-9]+/)?.[0];

    expect(runId).toBeDefined();
    const done = await manager.waitForRun(runId!);
    expect(done.result).toBe("retry-ok");
  });

  it("deletes completed runs", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "ok" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const first = manager.startRun({ script, mode: "run", maxAgents: 1 });
    await manager.waitForRun(first.id);

    const result = handleWorkflowsSlash(["delete", first.id], { workflowManager: manager });

    expect(result.info).toBe(`deleted workflow ${first.id}`);
    expect(manager.listRuns()).toEqual([]);
  });
});
