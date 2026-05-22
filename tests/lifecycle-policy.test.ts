import { describe, expect, it } from "vitest";
import { classifyLifecycleToolCall } from "../src/code/lifecycle-policy.js";
import { isHighRiskLifecycleToolCall, isLifecycleMutationToolCall } from "../src/code/lifecycle.js";

describe("lifecycle risk policy", () => {
  it("classifies safe read-only tools", () => {
    expect(classifyLifecycleToolCall("read_file", { path: "src/index.ts" })).toMatchObject({
      toolName: "read_file",
      risk: "safe",
      reason: "safe-tool",
    });
  });

  it("classifies ordinary edits as mutation but not high-risk", () => {
    expect(classifyLifecycleToolCall("edit_file", { path: "src/app.ts" })).toMatchObject({
      toolName: "edit_file",
      risk: "mutation",
      reason: "mutation-tool",
    });
  });

  it("classifies package and config edits as high-risk", () => {
    expect(classifyLifecycleToolCall("write_file", { path: "package.json" })).toMatchObject({
      risk: "high-risk",
      reason: "package-or-config-path",
    });
    expect(
      classifyLifecycleToolCall("edit_file", { path: ".github/workflows/ci.yml" }),
    ).toMatchObject({
      risk: "high-risk",
      reason: "package-or-config-path",
    });
  });

  it("classifies high-risk shell commands without flagging read-like commands", () => {
    expect(classifyLifecycleToolCall("run_command", { command: "npm install zod" })).toMatchObject({
      risk: "high-risk",
      reason: "high-risk-command",
    });
    expect(
      classifyLifecycleToolCall("run_command", { command: "git checkout -- README.md" }),
    ).toMatchObject({
      risk: "safe",
      reason: "default-safe",
    });
  });

  it("keeps existing lifecycle wrapper semantics", () => {
    expect(isHighRiskLifecycleToolCall("write_file", { path: "src/app.ts" })).toBe(false);
    expect(isLifecycleMutationToolCall("write_file", { path: "src/app.ts" })).toBe(true);
    expect(isHighRiskLifecycleToolCall("write_file", { path: "pnpm-lock.yaml" })).toBe(true);
    expect(isLifecycleMutationToolCall("run_command", { command: "npm test" })).toBe(false);
  });
});
