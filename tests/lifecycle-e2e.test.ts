import { describe, expect, it } from "vitest";
import { createStrictLifecycleHarness } from "./support/lifecycle-harness.js";

describe("strict engineering lifecycle e2e harness", () => {
  it("blocks high-risk mutation before an approved plan", async () => {
    const harness = createStrictLifecycleHarness();

    const rejected = await harness.dispatch("delete_file", { path: "src/old.ts" });

    expect(JSON.parse(rejected)).toMatchObject({
      rejectedReason: "engineering-lifecycle",
      state: "armed",
      nextAction: "submit_plan",
    });
  });

  it("runs a multi-step strict lifecycle from plan approval through final evidence", async () => {
    const harness = createStrictLifecycleHarness();

    const beforePlan = await harness.dispatch("delete_file", { path: "src/old.ts" });
    expect(JSON.parse(beforePlan)).toMatchObject({
      rejectedReason: "engineering-lifecycle",
      nextAction: "submit_plan",
    });

    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Refactor the formatter and refresh tests.",
      steps: [
        {
          id: "step-1",
          title: "Remove old formatter",
          action: "Delete the old formatter file.",
          risk: "high",
          targets: ["src/old.ts"],
          verification: ["npm test -- tests/lifecycle.test.ts"],
        },
        {
          id: "step-2",
          title: "Write replacement",
          action: "Create the new formatter module.",
          risk: "low",
          targets: ["src/format.ts"],
        },
      ],
    });
    expect(harness.lifecycle.snapshot().state).toBe("approved");

    const mutation = await harness.dispatch("delete_file", { path: "src/old.ts" });
    expect(mutation).toBe("deleted src/old.ts");
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      mutatedSinceLastStep: true,
    });

    const missingEvidence = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Removed the old formatter.",
    });
    expect(JSON.parse(missingEvidence)).toMatchObject({
      rejectedReason: "engineering-lifecycle-evidence",
      nextAction: "add_evidence",
    });

    harness.queue({ type: "continue" });
    const stepOneDone = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Removed the old formatter.",
      evidence: [
        {
          kind: "verification",
          summary: "focused lifecycle tests passed",
          command: "npm test -- tests/lifecycle.test.ts",
        },
      ],
    });
    expect(JSON.parse(stepOneDone)).toMatchObject({
      kind: "step_completed",
      stepId: "step-1",
      evidenceSummary: "verification: focused lifecycle tests passed",
    });
    expect(JSON.parse(stepOneDone).evidence).toBeUndefined();
    expect(harness.completions[0]?.evidence?.[0]).toMatchObject({
      command: "npm test -- tests/lifecycle.test.ts",
    });
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      completedStepIds: ["step-1"],
      mutatedSinceLastStep: false,
    });

    await harness.dispatch("write_file", { path: "src/format.ts", content: "export {};\n" });
    const lowRiskMissingEvidence = await harness.dispatch("mark_step_complete", {
      stepId: "step-2",
      result: "Created src/format.ts.",
    });
    expect(JSON.parse(lowRiskMissingEvidence)).toMatchObject({
      rejectedReason: "engineering-lifecycle-evidence",
      stepId: "step-2",
    });

    harness.queue({ type: "continue" });
    await harness.dispatch("mark_step_complete", {
      stepId: "step-2",
      result: "Created src/format.ts.",
      evidence: [{ kind: "diff", summary: "added src/format.ts", paths: ["src/format.ts"] }],
    });
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "complete",
      completedStepIds: ["step-1", "step-2"],
      mutatedSinceLastStep: false,
    });
  });

  it("covers a multi-file API refactor with move, cross-file edits, and verification evidence", async () => {
    const harness = createStrictLifecycleHarness();

    const beforePlan = await harness.dispatch("move_file", {
      source: "src/api/user.ts",
      destination: "src/api/users.ts",
    });
    expect(JSON.parse(beforePlan)).toMatchObject({
      rejectedReason: "engineering-lifecycle",
      state: "armed",
      nextAction: "submit_plan",
    });

    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Rename the user API module and update imports.",
      steps: [
        {
          id: "step-1",
          title: "Rename user API module",
          action: "Move the API module, update imports, and run focused tests.",
          risk: "med",
          targets: ["src/api/user.ts", "src/api/users.ts", "src/routes/user-route.ts"],
          acceptance: "All imports point at src/api/users.ts and focused API tests pass.",
          verification: ["npm test -- tests/api-user.test.ts"],
        },
      ],
    });

    const move = await harness.dispatch("move_file", {
      source: "src/api/user.ts",
      destination: "src/api/users.ts",
    });
    expect(move).toBe("moved src/api/user.ts → src/api/users.ts");

    const edits = await harness.dispatch("multi_edit", {
      edits: [
        {
          path: "src/routes/user-route.ts",
          search: "../api/user",
          replace: "../api/users",
        },
        {
          path: "src/api/users.ts",
          search: "export function getUser",
          replace: "export function getUsers",
        },
      ],
    });
    expect(edits).toBe("multi_edit: applied 2 edits across 2 files");
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      mutatedSinceLastStep: true,
    });

    const verification = await harness.dispatch("run_command", {
      command: "npm test -- tests/api-user.test.ts",
      cwd: "/repo",
    });
    expect(verification).toBe("exit 0\nnpm test -- tests/api-user.test.ts");

    const missingEvidence = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Renamed the user API module and updated imports.",
    });
    expect(JSON.parse(missingEvidence)).toMatchObject({
      rejectedReason: "engineering-lifecycle-evidence",
      stepId: "step-1",
      nextAction: "add_evidence",
    });

    harness.queue({ type: "continue" });
    const complete = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Renamed the user API module and updated imports.",
      evidence: [
        {
          kind: "diff",
          summary: "renamed API module and updated route imports",
          paths: ["src/api/user.ts", "src/api/users.ts", "src/routes/user-route.ts"],
        },
        {
          kind: "verification",
          summary: "focused API tests passed",
          command: "npm test -- tests/api-user.test.ts",
        },
      ],
    });

    expect(JSON.parse(complete)).toMatchObject({
      kind: "step_completed",
      stepId: "step-1",
      evidenceSummary:
        "diff: renamed API module and updated route imports; verification: focused API tests passed",
    });
    expect(harness.completions[0]?.evidence).toHaveLength(2);
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "complete",
      completedStepIds: ["step-1"],
      mutatedSinceLastStep: false,
    });
  });

  it("covers a config and dependency migration with package, lockfile, and install evidence", async () => {
    const blockedCalls: Array<{ name: string; args: Record<string, unknown> }> = [
      {
        name: "write_file",
        args: { path: "package.json", content: '{"dependencies":{"zod":"latest"}}\n' },
      },
      { name: "write_file", args: { path: "pnpm-lock.yaml", content: "lockfileVersion: '9.0'\n" } },
      {
        name: "write_file",
        args: { path: "tsconfig.json", content: '{"compilerOptions":{"strict":true}}\n' },
      },
      { name: "run_command", args: { command: "npm install zod", cwd: "/repo" } },
    ];

    for (const item of blockedCalls) {
      const harness = createStrictLifecycleHarness();
      const rejected = await harness.dispatch(item.name, item.args);

      expect(JSON.parse(rejected)).toMatchObject({
        rejectedReason: "engineering-lifecycle",
        state: "armed",
        nextAction: "submit_plan",
      });
    }

    const harness = createStrictLifecycleHarness();
    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Migrate validation dependency and strict TypeScript config.",
      steps: [
        {
          id: "step-1",
          title: "Update dependency manifests",
          action: "Update package.json, lockfile, and TypeScript config, then install deps.",
          risk: "high",
          targets: ["package.json", "pnpm-lock.yaml", "tsconfig.json"],
          acceptance: "Dependency manifests and TypeScript config are updated.",
          verification: ["npm install zod", "npm test -- tests/lifecycle.test.ts"],
        },
      ],
    });

    await harness.dispatch("write_file", {
      path: "package.json",
      content: '{"dependencies":{"zod":"latest"}}\n',
    });
    await harness.dispatch("write_file", {
      path: "pnpm-lock.yaml",
      content: "lockfileVersion: '9.0'\n",
    });
    await harness.dispatch("write_file", {
      path: "tsconfig.json",
      content: '{"compilerOptions":{"strict":true}}\n',
    });
    const install = await harness.dispatch("run_command", {
      command: "npm install zod",
      cwd: "/repo",
    });
    expect(install).toBe("exit 0\nnpm install zod");
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      mutatedSinceLastStep: true,
    });

    const missingEvidence = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Updated dependency manifests and TypeScript config.",
    });
    expect(JSON.parse(missingEvidence)).toMatchObject({
      rejectedReason: "engineering-lifecycle-evidence",
      stepId: "step-1",
      nextAction: "add_evidence",
    });

    harness.queue({ type: "continue" });
    const complete = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Updated dependency manifests and TypeScript config.",
      evidence: [
        {
          kind: "diff",
          summary: "updated dependency manifests and TypeScript config",
          paths: ["package.json", "pnpm-lock.yaml", "tsconfig.json"],
        },
        {
          kind: "verification",
          summary: "dependency install completed",
          command: "npm install zod",
        },
        {
          kind: "verification",
          summary: "lifecycle regression tests passed",
          command: "npm test -- tests/lifecycle.test.ts",
        },
      ],
    });

    expect(JSON.parse(complete)).toMatchObject({
      kind: "step_completed",
      stepId: "step-1",
      evidenceSummary:
        "diff: updated dependency manifests and TypeScript config; verification: dependency install completed; verification: lifecycle regression tests passed",
    });
    expect(harness.completions[0]?.evidence).toHaveLength(3);
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "complete",
      completedStepIds: ["step-1"],
      mutatedSinceLastStep: false,
    });
  });

  it("preserves completed prefix through an accepted revision", async () => {
    const harness = createStrictLifecycleHarness();
    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Refactor command routing.",
      steps: [
        { id: "step-1", title: "Extract router", action: "Move helpers.", risk: "low" },
        { id: "step-2", title: "Migrate callers", action: "Update call sites.", risk: "med" },
      ],
    });

    await harness.dispatch("write_file", { path: "src/router.ts", content: "export {};\n" });
    harness.queue({ type: "continue" });
    await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Extracted the router.",
      evidence: [{ kind: "diff", summary: "added router", paths: ["src/router.ts"] }],
    });

    harness.queue({ type: "accepted" });
    const revision = await harness.dispatch("revise_plan", {
      reason: "User asked to skip caller migration and document the follow-up.",
      remainingSteps: [
        {
          id: "step-3",
          title: "Document follow-up",
          action: "Document the skipped migration.",
          risk: "low",
        },
      ],
    });

    expect(revision).toBe("revision accepted");
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      completedStepIds: ["step-1"],
      planSteps: [
        { id: "step-1", title: "Extract router" },
        { id: "step-3", title: "Document follow-up" },
      ],
    });
  });

  it("covers failed verification followed by an accepted revised repair plan", async () => {
    const harness = createStrictLifecycleHarness({
      commandResults: {
        "npm test -- tests/router.test.ts":
          "exit 1\nFAIL tests/router.test.ts\nExpected route handler to use the new router.",
        "npm test -- tests/router-repair.test.ts":
          "exit 0\nnpm test -- tests/router-repair.test.ts",
      },
    });
    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Extract command routing and migrate callers.",
      steps: [
        {
          id: "step-1",
          title: "Extract router",
          action: "Move routing helpers into src/router.ts.",
          risk: "low",
          targets: ["src/router.ts"],
          verification: ["npm test -- tests/router.test.ts"],
        },
        {
          id: "step-2",
          title: "Migrate callers",
          action: "Update call sites to use the extracted router.",
          risk: "med",
          targets: ["src/app.ts", "src/cli.ts"],
          verification: ["npm test -- tests/router.test.ts"],
        },
      ],
    });

    await harness.dispatch("write_file", { path: "src/router.ts", content: "export {};\n" });
    harness.queue({ type: "continue" });
    await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Extracted the router module.",
      evidence: [{ kind: "diff", summary: "added router module", paths: ["src/router.ts"] }],
    });

    const failedVerification = await harness.dispatch("run_command", {
      command: "npm test -- tests/router.test.ts",
      cwd: "/repo",
    });
    expect(failedVerification).toContain("exit 1");
    expect(failedVerification).toContain("Expected route handler to use the new router.");

    harness.queue({ type: "accepted" });
    const revision = await harness.dispatch("revise_plan", {
      reason:
        "Focused router tests failed; repair the route expectations before migrating callers.",
      remainingSteps: [
        {
          id: "step-2",
          title: "Repair router test expectations",
          action: "Update the failing route test to match the extracted router.",
          risk: "low",
          targets: ["tests/router.test.ts"],
          verification: ["npm test -- tests/router-repair.test.ts"],
        },
        {
          id: "step-3",
          title: "Migrate callers after repair",
          action: "Update callers once the router contract is green.",
          risk: "med",
          targets: ["src/app.ts", "src/cli.ts"],
          verification: ["npm test -- tests/router-repair.test.ts"],
        },
      ],
    });

    expect(revision).toBe("revision accepted");
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      completedStepIds: ["step-1"],
      mutatedSinceLastStep: false,
      planSteps: [
        { id: "step-1", title: "Extract router" },
        { id: "step-2", title: "Repair router test expectations" },
        { id: "step-3", title: "Migrate callers after repair" },
      ],
    });

    await harness.dispatch("write_file", {
      path: "tests/router.test.ts",
      content: "expect(route.handler).toBe(router.handler);\n",
    });
    const missingEvidence = await harness.dispatch("mark_step_complete", {
      stepId: "step-2",
      result: "Updated router test expectations.",
    });
    expect(JSON.parse(missingEvidence)).toMatchObject({
      rejectedReason: "engineering-lifecycle-evidence",
      stepId: "step-2",
      nextAction: "add_evidence",
    });

    const repairedVerification = await harness.dispatch("run_command", {
      command: "npm test -- tests/router-repair.test.ts",
      cwd: "/repo",
    });
    expect(repairedVerification).toBe("exit 0\nnpm test -- tests/router-repair.test.ts");

    harness.queue({ type: "continue" });
    await harness.dispatch("mark_step_complete", {
      stepId: "step-2",
      result: "Updated router test expectations.",
      evidence: [
        { kind: "diff", summary: "repaired router test", paths: ["tests/router.test.ts"] },
        {
          kind: "verification",
          summary: "router repair tests passed",
          command: "npm test -- tests/router-repair.test.ts",
        },
      ],
    });

    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "executing",
      completedStepIds: ["step-1", "step-2"],
      mutatedSinceLastStep: false,
    });
  });

  it("cancels the runtime when the user stops at a checkpoint", async () => {
    const harness = createStrictLifecycleHarness();
    harness.queue({ type: "approve" });
    await harness.dispatch("submit_plan", {
      plan: "Remove old formatter.",
      steps: [
        {
          id: "step-1",
          title: "Remove old formatter",
          action: "Delete old formatter.",
          risk: "high",
        },
      ],
    });
    await harness.dispatch("delete_file", { path: "src/old-format.ts" });

    harness.queue({ type: "stop" });
    const stopped = await harness.dispatch("mark_step_complete", {
      stepId: "step-1",
      result: "Removed old formatter.",
      evidence: [{ kind: "manual", summary: "user wants to stop before continuing" }],
    });

    expect(JSON.parse(stopped).error).toMatch(/user stopped at checkpoint/);
    expect(harness.lifecycle.snapshot()).toMatchObject({
      state: "cancelled",
      planSteps: [],
      completedStepIds: [],
      mutatedSinceLastStep: false,
    });
    const afterStop = await harness.dispatch("delete_file", { path: "src/another-old-file.ts" });
    expect(JSON.parse(afterStop)).toMatchObject({
      rejectedReason: "engineering-lifecycle",
      state: "cancelled",
    });
  });
});
