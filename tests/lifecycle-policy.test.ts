import { describe, expect, it } from "vitest";
import { classifyLifecycleToolCall } from "../src/code/lifecycle-policy.js";
import { isHighRiskLifecycleToolCall, isLifecycleMutationToolCall } from "../src/code/lifecycle.js";

describe("lifecycle risk policy", () => {
  const cases: Array<{
    name: string;
    toolName: string;
    args: Record<string, unknown>;
    risk: "safe" | "mutation" | "high-risk";
    reason?: string;
  }> = [
    {
      name: "read-only file exploration",
      toolName: "read_file",
      args: { path: "src/index.ts" },
      risk: "safe",
      reason: "safe-tool",
    },
    {
      name: "read-only search",
      toolName: "search_content",
      args: { pattern: "registerPlanTool" },
      risk: "safe",
      reason: "safe-tool",
    },
    {
      name: "ordinary source edit",
      toolName: "edit_file",
      args: { path: "src/app.ts" },
      risk: "mutation",
      reason: "mutation-tool",
    },
    {
      name: "ordinary docs write",
      toolName: "write_file",
      args: { path: "docs/notes.md", content: "notes\n" },
      risk: "mutation",
      reason: "mutation-tool",
    },
    {
      name: "batch edit",
      toolName: "multi_edit",
      args: { edits: [{ path: "src/a.ts", search: "a", replace: "b" }] },
      risk: "high-risk",
      reason: "high-risk-tool",
    },
    {
      name: "file move",
      toolName: "move_file",
      args: { source: "src/a.ts", destination: "src/b.ts" },
      risk: "high-risk",
      reason: "high-risk-tool",
    },
    {
      name: "package manifest write",
      toolName: "write_file",
      args: { path: "package.json", content: "{}\n" },
      risk: "high-risk",
      reason: "package-or-config-path",
    },
    {
      name: "lockfile edit",
      toolName: "edit_file",
      args: { path: "pnpm-lock.yaml" },
      risk: "high-risk",
      reason: "package-or-config-path",
    },
    {
      name: "typescript config edit",
      toolName: "edit_file",
      args: { path: "tsconfig.build.json" },
      risk: "high-risk",
      reason: "package-or-config-path",
    },
    {
      name: "workflow edit",
      toolName: "write_file",
      args: { path: ".github/workflows/ci.yml", content: "name: ci\n" },
      risk: "high-risk",
      reason: "package-or-config-path",
    },
    {
      name: "safe test command",
      toolName: "run_command",
      args: { command: "npm test -- tests/lifecycle.test.ts" },
      risk: "safe",
      reason: "default-safe",
    },
    {
      name: "safe git status",
      toolName: "run_command",
      args: { command: "git status --short" },
      risk: "safe",
      reason: "default-safe",
    },
    {
      name: "safe git path checkout",
      toolName: "run_command",
      args: { command: "git checkout -- README.md" },
      risk: "safe",
      reason: "default-safe",
    },
    {
      name: "npm install dependency mutation",
      toolName: "run_command",
      args: { command: "npm install zod" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "npm uninstall dependency mutation",
      toolName: "run_command",
      args: { command: "npm uninstall left-pad" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "pnpm add dependency mutation",
      toolName: "run_command",
      args: { command: "pnpm add zod" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "pnpm up dependency mutation",
      toolName: "run_command",
      args: { command: "pnpm up zod" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "yarn remove dependency mutation",
      toolName: "run_command",
      args: { command: "yarn remove left-pad" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "yarn upgrade dependency mutation",
      toolName: "run_command",
      args: { command: "yarn upgrade zod" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "destructive rm command",
      toolName: "run_command",
      args: { command: "rm -rf dist" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "destructive git reset",
      toolName: "run_command",
      args: { command: "git reset --hard HEAD" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "destructive git clean",
      toolName: "run_command",
      args: { command: "git clean -fd" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "git push",
      toolName: "run_command",
      args: { command: "git push origin main" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "git branch checkout",
      toolName: "run_command",
      args: { command: "git checkout feature/lifecycle" },
      risk: "high-risk",
      reason: "high-risk-command",
    },
    {
      name: "safe quoted destructive-looking text",
      toolName: "run_command",
      args: { command: "echo 'rm -rf dist'" },
      risk: "safe",
      reason: "default-safe",
    },
  ];

  it.each(cases)("classifies corpus case: $name", ({ toolName, args, risk, reason }) => {
    const decision = classifyLifecycleToolCall(toolName, args);
    expect(decision.risk).toBe(risk);
    if (reason) expect(decision.reason).toBe(reason);
  });

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
