export type DecisionSurfaceKind =
  | "tool_approval"
  | "plan_approval"
  | "ask"
  | "workspace_conflict"
  | "mode_jobs"
  | "close_active"
  | "clear_context"
  | "mcp_interaction";

export const DECISION_SURFACE_MOCK_TRIGGERS: Readonly<Record<DecisionSurfaceKind, string>> = {
  tool_approval: "/mock-tool-approval",
  plan_approval: "/mock-plan-approval",
  ask: "/mock-ask",
  workspace_conflict: "/mock-workspace-conflict",
  mode_jobs: "/mock-mode-jobs",
  close_active: "/mock-close-active",
  clear_context: "/mock-clear-context",
  mcp_interaction: "/mock-mcp-interaction",
};

// QA-only stress scene. Keep this separate from the eight product decision
// surfaces so the canonical inventory stays stable while copy/layout extremes
// can be exercised through the same Ask card used in production.
export const LONG_DECISION_OPTIONS_MOCK_TRIGGER = "/mock-long-options";

const aliases: Readonly<Record<string, DecisionSurfaceKind>> = {
  ...Object.fromEntries(
    Object.entries(DECISION_SURFACE_MOCK_TRIGGERS).map(([kind, trigger]) => [trigger, kind]),
  ) as Record<string, DecisionSurfaceKind>,
  "/approve-preview": "tool_approval",
  "approve preview": "tool_approval",
  "approve预览": "tool_approval",
  "/plan-approve-preview": "plan_approval",
  "plan approve preview": "plan_approval",
  "plan approve预览": "plan_approval",
  "/ask-preview": "ask",
  "ask preview": "ask",
  "ask预览": "ask",
  "mock 工具审批": "tool_approval",
  "mock 计划审批": "plan_approval",
  "mock 提问": "ask",
  "mock 工作区冲突": "workspace_conflict",
  "mock 模式切换": "mode_jobs",
  "mock 关闭任务": "close_active",
  "mock 清空上下文": "clear_context",
};

export function decisionSurfaceMockFromInput(input: string): DecisionSurfaceKind | null {
  return aliases[input.trim().toLowerCase()] ?? null;
}

export function isLongDecisionOptionsMockInput(input: string): boolean {
  const normalized = input.trim().toLowerCase();
  return normalized === LONG_DECISION_OPTIONS_MOCK_TRIGGER || normalized === "mock 长文案选项";
}
