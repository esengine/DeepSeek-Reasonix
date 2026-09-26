import type { DecisionSurfaceKind as MockDecisionSurfaceKind } from "../lib/decisionSurfaceMock";
import type { State } from "../lib/useController";
import type { ActiveWorkView, WorkspaceConflictView } from "../lib/types";

export type AppDecisionSurfaceKind = MockDecisionSurfaceKind | "extension_form";

type PendingClose = { tabId: string; work: ActiveWorkView; stopping: boolean } | null;

/**
 * Single footer decision surface precedence. Composer stays mounted
 * underneath and is only visually/a11y-hidden so per-session draft caches
 * survive.
 */
export function projectDecisionSurface(input: {
  approval: State["approval"];
  ask: State["ask"];
  mcpInteraction: State["mcpInteraction"];
  extensionForm: State["extensionForm"];
  workspaceConflict: WorkspaceConflictView | null;
  pendingClose: PendingClose;
  clearContextPending: boolean;
  remote?: boolean;
}): AppDecisionSurfaceKind | null {
  // A remote session's prompts and forms are answered by RemoteSessionSurface,
  // the one owner of the serve's exact prompt route; a footer copy would be a
  // second card whose answer has no local session to reach.
  if (!input.remote) {
    const prompt = projectPromptSurface(input);
    if (prompt) return prompt;
  }
  if (input.workspaceConflict) return "workspace_conflict";
  if (input.pendingClose) return "close_active";
  if (input.clearContextPending) return "clear_context";
  return null;
}

function projectPromptSurface(input: {
  approval: State["approval"];
  ask: State["ask"];
  mcpInteraction: State["mcpInteraction"];
  extensionForm: State["extensionForm"];
}): AppDecisionSurfaceKind | null {
  if (input.approval) {
    return input.approval.tool === "exit_plan_mode" ? "plan_approval" : "tool_approval";
  }
  if (input.ask) return "ask";
  if (input.mcpInteraction) return "mcp_interaction";
  if (input.extensionForm) return "extension_form";
  return null;
}
