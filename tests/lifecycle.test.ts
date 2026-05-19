import { describe, expect, it } from "vitest";
import {
  EngineeringLifecycleRuntime,
  classifyEngineeringPrompt,
  isHighRiskLifecycleToolCall,
} from "../src/code/lifecycle.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";

describe("engineering lifecycle classifier", () => {
  it("keeps small single-file fixes lightweight", () => {
    expect(classifyEngineeringPrompt("Fix the typo in README.md")).toBe("small");
    expect(classifyEngineeringPrompt("Explain how /plan works")).toBe("small");
  });

  it("arms for large engineering prompts", () => {
    expect(classifyEngineeringPrompt("Refactor the auth layer across frontend and backend")).toBe(
      "large",
    );
    expect(classifyEngineeringPrompt("Implement a migration that changes the public API")).toBe(
      "large",
    );
  });
});

describe("engineering lifecycle high-risk tool detection", () => {
  it("treats read-only exploration as safe", () => {
    expect(isHighRiskLifecycleToolCall("read_file", { path: "src/index.ts" })).toBe(false);
    expect(isHighRiskLifecycleToolCall("search_content", { pattern: "foo" })).toBe(false);
  });

  it("treats batch edits and destructive filesystem calls as high risk", () => {
    expect(
      isHighRiskLifecycleToolCall("multi_edit", {
        edits: [
          { path: "src/a.ts", search: "a", replace: "b" },
          { path: "src/b.ts", search: "a", replace: "b" },
        ],
      }),
    ).toBe(true);
    expect(isHighRiskLifecycleToolCall("delete_file", { path: "src/a.ts" })).toBe(true);
  });
});

describe("EngineeringLifecycleRuntime", () => {
  it("blocks high-risk mutations before an approved plan", () => {
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "auto" });
    lifecycle.observeUserPrompt("Refactor the shell and filesystem tool gates");

    const out = lifecycle.guardToolCall("multi_edit", {
      edits: [
        { path: "src/a.ts", search: "a", replace: "b" },
        { path: "src/b.ts", search: "a", replace: "b" },
      ],
    });

    expect(out).not.toBeNull();
    expect(JSON.parse(out!).rejectedReason).toBe("engineering-lifecycle");
    expect(lifecycle.snapshot().state).toBe("armed");
  });

  it("allows high-risk mutations after plan approval and then requires step evidence", () => {
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "auto" });
    lifecycle.observeUserPrompt("Refactor the shell and filesystem tool gates");
    lifecycle.recordPlanApproved([
      {
        id: "step-1",
        title: "Refactor gates",
        action: "Change multiple tool gate files.",
        risk: "med",
      },
    ]);

    expect(
      lifecycle.guardToolCall("multi_edit", {
        edits: [
          { path: "src/a.ts", search: "a", replace: "b" },
          { path: "src/b.ts", search: "a", replace: "b" },
        ],
      }),
    ).toBeNull();

    const rejected = lifecycle.guardToolCall("mark_step_complete", {
      stepId: "step-1",
      result: "Refactored the gate path.",
    });
    expect(rejected).not.toBeNull();
    expect(JSON.parse(rejected!).error).toMatch(/evidence/);

    expect(
      lifecycle.guardToolCall("mark_step_complete", {
        stepId: "step-1",
        result: "Refactored the gate path.",
        evidence: [{ kind: "verification", summary: "npm test tests/lifecycle.test.ts passed" }],
      }),
    ).toBeNull();
  });

  it("requires evidence for low-risk steps after a successful code mutation", () => {
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "auto" });
    lifecycle.observeUserPrompt("Refactor formatting across modules");
    lifecycle.recordPlanApproved([
      {
        id: "step-1",
        title: "Extract formatter",
        action: "Move formatting into src/format.ts.",
        risk: "low",
        targets: ["src/format.ts"],
      },
    ]);

    lifecycle.recordToolResult(
      "write_file",
      { path: "src/format.ts" },
      "▸ edit blocks: 1/1 applied\n  ✓ created     src/format.ts",
    );

    const rejected = lifecycle.guardToolCall("mark_step_complete", {
      stepId: "step-1",
      result: "Created src/format.ts.",
    });
    expect(rejected).not.toBeNull();
    expect(JSON.parse(rejected!).rejectedReason).toBe("engineering-lifecycle-evidence");
  });

  it("does not require mutation evidence when an edit was rejected before touching disk", () => {
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "auto" });
    lifecycle.observeUserPrompt("Refactor formatting across modules");
    lifecycle.recordPlanApproved([
      {
        id: "step-1",
        title: "Try formatter",
        action: "Attempt a formatter edit.",
        risk: "low",
      },
    ]);

    lifecycle.recordToolResult(
      "edit_file",
      { path: "src/app.ts" },
      "User rejected this edit to src/app.ts. Don't retry the same SEARCH/REPLACE.",
    );

    expect(
      lifecycle.guardToolCall("mark_step_complete", {
        stepId: "step-1",
        result: "No code was changed.",
      }),
    ).toBeNull();
  });

  it("does not mutate the immutable prefix as lifecycle state changes", () => {
    const prefix = new ImmutablePrefix({ system: "s", toolSpecs: [] });
    const before = prefix.fingerprint;
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "strict" });

    lifecycle.observeUserPrompt("Refactor everything");
    lifecycle.recordPlanApproved([
      { id: "step-1", title: "Do work", action: "Do high risk work.", risk: "high" },
    ]);
    lifecycle.guardToolCall("delete_file", { path: "src/old.ts" });

    expect(prefix.verifyFingerprint()).toBe(before);
  });

  it("starts a fresh lifecycle after cancellation or completion", () => {
    const lifecycle = new EngineeringLifecycleRuntime({ mode: "auto" });
    lifecycle.observeUserPrompt("Refactor the command router");
    lifecycle.cancel();

    lifecycle.observeUserPrompt("Fix the typo in README.md");
    expect(lifecycle.snapshot().state).toBe("idle");

    lifecycle.observeUserPrompt("Refactor the command router again");
    lifecycle.recordPlanApproved([
      { id: "step-1", title: "Refactor", action: "Refactor command routing.", risk: "low" },
    ]);
    lifecycle.recordStepCompleted("step-1");
    expect(lifecycle.snapshot().state).toBe("complete");

    lifecycle.observeUserPrompt("Fix another typo");
    expect(lifecycle.snapshot().state).toBe("idle");
  });
});
