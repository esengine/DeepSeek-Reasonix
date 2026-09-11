import type { Meta, TabMeta } from "../../lib/types";

export function tabMeta(overrides: Partial<TabMeta> = {}): TabMeta {
  return {
    id: "tab-a",
    scope: "project",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    topicId: "topic-a",
    topicTitle: "General",
    label: "model",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    active: true,
    cwd: "/repo",
    ...overrides,
  };
}

export function meta(overrides: Partial<Meta> = {}): Meta {
  return {
    label: "model",
    ready: true,
    eventChannel: "agent:event",
    cwd: "/repo",
    workspaceRoot: "/repo",
    workspaceName: "repo",
    workspacePath: "/repo",
    gitBranch: "main",
    autoApproveTools: false,
    bypass: false,
    collaborationMode: "normal",
    toolApprovalMode: "ask",
    tokenMode: "full",
    goal: "",
    goalStatus: "stopped",
    ...overrides,
  };
}
