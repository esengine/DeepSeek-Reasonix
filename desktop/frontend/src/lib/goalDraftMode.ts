import {
  normalizeCollaborationMode,
  type CollaborationMode,
  type Mode,
} from "./types";

export function keepGoalDraftMode(current: boolean, goal?: string): boolean {
  return current && !(goal ?? "").trim();
}

export function displayedCollaborationMode(params: {
  goalDraftMode: boolean;
  localMode?: CollaborationMode;
  metaGoal?: string;
  tabMode?: string;
  goal?: string;
  legacyMode?: Mode;
}): CollaborationMode {
  if (params.goalDraftMode) return "goal";
  return params.localMode ?? normalizeCollaborationMode(params.metaGoal ? "goal" : params.tabMode, params.goal, params.legacyMode);
}

export function tabListCollaborationMode(params: {
  goalDraftMode: boolean;
  localMode?: CollaborationMode;
  tabMode?: string;
  tabGoal?: string;
  legacyMode?: Mode;
}): CollaborationMode {
  if (params.goalDraftMode) return "goal";
  return params.localMode ?? normalizeCollaborationMode(params.tabMode, params.tabGoal, params.legacyMode);
}

export function metaSyncedCollaborationMode(params: {
  nextGoal?: string;
  goalDraftMode: boolean;
  legacyMode?: Mode;
}): CollaborationMode {
  return params.nextGoal || params.goalDraftMode
    ? "goal"
    : normalizeCollaborationMode(undefined, "", params.legacyMode);
}

export function controllerCollaborationMode(params: {
  collaborationMode: CollaborationMode;
  goal?: string;
}): CollaborationMode {
  return params.collaborationMode === "goal" && !params.goal?.trim()
    ? "normal"
    : params.collaborationMode;
}
