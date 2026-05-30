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

  it("treats /workflows list as the explicit list form", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "ok" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });
    await manager.waitForRun(started.id);

    const result = handleWorkflowsSlash(["list"], { workflowManager: manager });

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

  it("attaches a completed workflow run to the current conversation", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "finding: unsafe parser" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });
    await manager.waitForRun(started.id);
    const messages: Array<{ role: string; content: string }> = [];

    const result = handleWorkflowsSlash(
      ["attach", started.id],
      { workflowManager: manager },
      { appendAndPersist: (message) => messages.push(message) },
    );

    expect(result.info).toContain(`attached workflow ${started.id}`);
    expect(messages).toHaveLength(1);
    expect(messages[0]?.role).toBe("system");
    expect(messages[0]?.content).toContain("finding: unsafe parser");
  });

  it("continues from a completed workflow run with a follow-up instruction", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: true, output: "finding: missing tests" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({ script, mode: "run" });
    await manager.waitForRun(started.id);
    const messages: Array<{ role: string; content: string }> = [];

    const result = handleWorkflowsSlash(
      ["continue", started.id, "fix", "the", "issues"],
      { workflowManager: manager },
      { appendAndPersist: (message) => messages.push(message) },
    );

    expect(messages[0]?.content).toContain("finding: missing tests");
    expect(result.resubmit).toBe("fix the issues");
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

  it("passes run options when starting a saved workflow", async () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-workflows-slash-options-"));
    const home = mkdtempSync(join(tmpdir(), "reasonix-workflows-home-options-"));
    try {
      mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
      writeFileSync(join(root, ".reasonix", "workflows", "audit.js"), script, "utf8");
      const runner: WorkflowAgentRunner = {
        async run() {
          return { ok: true, output: "unreachable" };
        },
      };
      const manager = new WorkflowRunManager({ runner, rootDir: root, homeDir: home });

      const result = handleWorkflowsSlash(
        [
          "run",
          "audit",
          "scope",
          "src/workflow",
          "--concurrency",
          "5",
          "--max-agents",
          "9",
          "--mode",
          "dry_run",
          "--background",
          "--tool-mode",
          "full",
        ],
        {
          workflowManager: manager,
          codeRoot: root,
          homeDir: home,
        },
      );
      const runId = result.info?.match(/wf_[a-z0-9]+/)?.[0];

      expect(runId).toBeDefined();
      const done = await manager.waitForRun(runId!);
      expect(done.mode).toBe("dry_run");
      expect(done.background).toBe(true);
      expect(done.concurrency).toBe(5);
      expect(done.maxAgents).toBe(9);
      expect(done.toolMode).toBe("full");
      expect(done.result).toBe("dry-run:inspection");
    } finally {
      rmSync(root, { recursive: true, force: true });
      rmSync(home, { recursive: true, force: true });
    }
  });

  it("rejects invalid saved workflow run options", () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-workflows-slash-invalid-"));
    const home = mkdtempSync(join(tmpdir(), "reasonix-workflows-home-invalid-"));
    try {
      mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
      writeFileSync(join(root, ".reasonix", "workflows", "audit.js"), script, "utf8");
      const manager = new WorkflowRunManager({ rootDir: root, homeDir: home });
      const ctx = { workflowManager: manager, codeRoot: root, homeDir: home };

      expect(handleWorkflowsSlash(["run", "audit", "--concurrency", "0"], ctx).info).toMatch(
        /concurrency/,
      );
      expect(handleWorkflowsSlash(["run", "audit", "--max-agents", "abc"], ctx).info).toMatch(
        /max-agents/,
      );
      expect(handleWorkflowsSlash(["run", "audit", "--mode", "bad"], ctx).info).toMatch(/mode/);
      expect(handleWorkflowsSlash(["run", "audit", "--tool-mode", "bad"], ctx).info).toMatch(
        /tool-mode/,
      );
      expect(handleWorkflowsSlash(["run", "audit", "--bogus"], ctx).info).toMatch(
        /unknown workflow run option/,
      );
      expect(manager.listRuns()).toEqual([]);
    } finally {
      rmSync(root, { recursive: true, force: true });
      rmSync(home, { recursive: true, force: true });
    }
  });

  it("uses a tool-mode-specific runner when provided", async () => {
    const root = mkdtempSync(join(tmpdir(), "reasonix-workflows-slash-tool-mode-"));
    const home = mkdtempSync(join(tmpdir(), "reasonix-workflows-home-tool-mode-"));
    const modes: string[] = [];
    try {
      mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
      writeFileSync(join(root, ".reasonix", "workflows", "audit.js"), script, "utf8");
      const manager = new WorkflowRunManager({ rootDir: root, homeDir: home });

      const result = handleWorkflowsSlash(["run", "audit", "--tool-mode", "full"], {
        workflowManager: manager,
        codeRoot: root,
        homeDir: home,
        workflowRunnerForToolMode: (mode) => {
          modes.push(mode);
          return {
            async run(prompt) {
              return { ok: true, output: `${mode}:${prompt}` };
            },
          };
        },
      });
      const runId = result.info?.match(/wf_[a-z0-9]+/)?.[0];

      expect(runId).toBeDefined();
      const done = await manager.waitForRun(runId!);
      expect(modes).toEqual(["full"]);
      expect(done.result).toBe("full:inspect");
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

  it("shows expanded workflow run details", async () => {
    const runner: WorkflowAgentRunner = {
      async run() {
        return { ok: false, output: "", error: "scan failed" };
      },
    };
    const manager = new WorkflowRunManager({ runner, rootDir: process.cwd() });
    const started = manager.startRun({
      script: `export const meta = {
  name: "detailed_audit",
  description: "Detailed audit",
  phases: [{ title: "Scan" }],
}
phase("Scan")
const value = await agent("inspect", { label: "inspection" })
throw new Error("script failed after " + value)
`,
      mode: "run",
      concurrency: 4,
      maxAgents: 6,
      modelPolicy: "pro",
    });
    await manager.waitForRun(started.id);

    const result = handleWorkflowsSlash(["show", started.id], { workflowManager: manager });

    expect(result.info).toContain("name: detailed_audit");
    expect(result.info).toContain("description: Detailed audit");
    expect(result.info).toContain("mode: run");
    expect(result.info).toContain("background: false");
    expect(result.info).toContain("concurrency: 4");
    expect(result.info).toContain("max_agents: 6");
    expect(result.info).toContain("model_policy: pro");
    expect(result.info).toContain("phases:");
    expect(result.info).toContain("- Scan agents=1");
    expect(result.info).toContain("agents:");
    expect(result.info).toContain("- failed inspection");
    expect(result.info).toContain("error=scan failed");
    expect(result.info).toContain("error (script): Error: script failed");
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
