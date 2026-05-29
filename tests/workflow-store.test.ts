import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { WorkflowRunStore } from "../src/workflow/store.js";
import type { WorkflowRunSnapshot } from "../src/workflow/types.js";

function snapshot(id = "wf_test"): WorkflowRunSnapshot {
  return {
    id,
    name: "audit",
    description: "Audit",
    status: "completed",
    mode: "run",
    startedAt: 100,
    updatedAt: 150,
    durationMs: 50,
    phases: [{ title: "Explore", startedAt: 100, agentCount: 1 }],
    agents: [
      {
        id: `${id}_a1`,
        label: "reader",
        status: "completed",
        promptPreview: "Read files",
        outputPreview: "ok",
        startedAt: 105,
        completedAt: 140,
        durationMs: 35,
      },
    ],
    logs: ["done"],
    result: { ok: true },
    agentCount: 1,
    script: `export const meta = { name: "audit", description: "Audit" }\nreturn true\n`,
  };
}

describe("WorkflowRunStore", () => {
  let root: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-wf-store-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("persists and loads workflow run snapshots", () => {
    const store = new WorkflowRunStore({ rootDir: root });
    store.save(snapshot("wf_one"));

    const loaded = new WorkflowRunStore({ rootDir: root }).loadAll();
    expect(loaded).toHaveLength(1);
    expect(loaded[0]).toMatchObject({
      id: "wf_one",
      name: "audit",
      status: "completed",
      agentCount: 1,
    });
  });

  it("marks previously running snapshots as failed on load", () => {
    const store = new WorkflowRunStore({ rootDir: root });
    store.save({ ...snapshot("wf_running"), status: "running", error: undefined });

    const loaded = new WorkflowRunStore({ rootDir: root }).loadAll();
    expect(loaded[0]).toMatchObject({
      id: "wf_running",
      status: "failed",
      error: "workflow interrupted before completion",
      errorKind: "internal",
    });
    expect(loaded[0]?.agents[0]).toMatchObject({ status: "completed" });
  });

  it("deletes a persisted snapshot by id", () => {
    const store = new WorkflowRunStore({ rootDir: root });
    store.save(snapshot("wf_delete"));
    expect(store.delete("wf_delete")).toBe(true);
    expect(store.loadAll()).toEqual([]);
  });
});
