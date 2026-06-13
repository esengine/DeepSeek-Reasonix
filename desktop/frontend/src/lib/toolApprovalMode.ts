import type { ToolApprovalMode } from "./types";

export type RestorableToolApprovalMode = Exclude<ToolApprovalMode, "yolo">;

export function restorableToolApprovalMode(mode?: ToolApprovalMode): RestorableToolApprovalMode {
  return mode === "auto" ? "auto" : "ask";
}

export function toggleYoloToolApprovalMode(
  current: ToolApprovalMode,
  restore?: ToolApprovalMode,
): { mode: ToolApprovalMode; restore?: RestorableToolApprovalMode } {
  if (current === "yolo") {
    return { mode: restorableToolApprovalMode(restore) };
  }
  return { mode: "yolo", restore: restorableToolApprovalMode(current) };
}
