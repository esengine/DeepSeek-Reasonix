import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { WorkflowRunManager } from "../src/workflow/manager.js";
import type { WorkflowAgentRunner } from "../src/workflow/types.js";

const script = `export const meta = { name: "persisted", description: "Persisted workflow" }
const result = await agent({ label: "reader", instruction: "read", type: "verify" })
return { result }
`;

describe("WorkflowRunManager persistence", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-wf-manager-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("loads completed background runs in a new manager", async () => {
    const runner: WorkflowAgentRunner = {
      run: async () => ({ ok: true, output: "OK" }),
    };
    const manager = new WorkflowRunManager({ rootDir: root, runner, persist: true });
    const started = manager.startRun({ script, mode: "run", maxAgents: 1 });
    await manager.waitForRun(started.id);

    const reloaded = new WorkflowRunManager({ rootDir: root, runner, persist: true });
    expect(reloaded.listRuns()).toHaveLength(1);
    expect(reloaded.getRun(started.id)).toMatchObject({
      id: started.id,
      status: "completed",
      result: { result: "OK" },
    });
  });

  it("deletes completed persisted runs", async () => {
    const runner: WorkflowAgentRunner = {
      run: async () => ({ ok: true, output: "OK" }),
    };
    const manager = new WorkflowRunManager({ rootDir: root, runner, persist: true });
    const started = manager.startRun({ script, mode: "run", maxAgents: 1 });
    await manager.waitForRun(started.id);

    expect(manager.deleteRun(started.id)).toBe(true);
    expect(manager.listRuns()).toEqual([]);
    expect(new WorkflowRunManager({ rootDir: root, runner, persist: true }).listRuns()).toEqual([]);
  });
});
