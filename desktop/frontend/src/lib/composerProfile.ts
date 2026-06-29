import {
  modeFromAxes,
  modeHasAutoApproveTools,
  modeHasPlan,
  normalizeCollaborationMode,
  normalizeMode,
  normalizeTokenMode,
  normalizeToolApprovalMode,
  type CollaborationMode,
  type GoalStatus,
  type Meta,
  type Mode,
  type TabMeta,
  type TokenMode,
  type ToolApprovalMode,
} from "./types";

export type ComposerProfileField = "collaborationMode" | "toolApprovalMode" | "tokenMode" | "goal";

export type ComposerProfilePending = Partial<Record<ComposerProfileField, true>>;

export interface ComposerProfile {
  collaborationMode: CollaborationMode;
  goalDraftMode: boolean;
  toolApprovalMode: ToolApprovalMode;
  tokenMode: TokenMode;
  goal: string;
  pending: ComposerProfilePending;
}

export type ComposerProfilesByTab = Record<string, ComposerProfile>;
export type UserPlanModeIntents = Record<string, true>;

const profileFields: ComposerProfileField[] = ["collaborationMode", "toolApprovalMode", "tokenMode", "goal"];

export const defaultComposerProfile: ComposerProfile = Object.freeze({
  collaborationMode: "normal",
  goalDraftMode: false,
  toolApprovalMode: "ask",
  tokenMode: "full",
  goal: "",
  pending: {},
});

function activeGoal(goal?: string, status?: GoalStatus): string {
  const trimmed = (goal ?? "").trim();
  if (!trimmed) return "";
  if (status && status !== "running") return "";
  return trimmed;
}

function profileWithPending(profile: Omit<ComposerProfile, "pending">, pending: ComposerProfilePending = {}): ComposerProfile {
  return { ...profile, pending };
}

function fallbackToolApprovalMode(rawMode: string | undefined, fallback?: ToolApprovalMode | null): ToolApprovalMode | undefined {
  if ((rawMode ?? "").trim() !== "") return undefined;
  return fallback === "auto" ? "auto" : undefined;
}

export function composerProfileFromTab(tab?: TabMeta | null, fallback?: ToolApprovalMode | null): ComposerProfile {
  if (!tab) return { ...defaultComposerProfile, pending: {} };
  const legacyMode = normalizeMode(tab.mode);
  const goal = activeGoal(tab.goal, tab.goalStatus);
  return profileWithPending({
    collaborationMode: normalizeCollaborationMode(tab.collaborationMode, goal, legacyMode),
    goalDraftMode: false,
    toolApprovalMode: normalizeToolApprovalMode(
      tab.toolApprovalMode,
      legacyMode,
      tab.toolApprovalMode === "yolo",
      fallbackToolApprovalMode(tab.toolApprovalMode, fallback),
    ),
    tokenMode: normalizeTokenMode(tab.tokenMode),
    goal,
  });
}

export function composerProfileFromMeta(meta?: Meta | null, legacyMode?: Mode, fallback?: ToolApprovalMode | null): ComposerProfile {
  if (!meta) return { ...defaultComposerProfile, pending: {} };
  const fallbackMode = normalizeMode(legacyMode);
  const goal = activeGoal(meta.goal, meta.goalStatus);
  const toolApprovalMode = normalizeToolApprovalMode(
    meta.toolApprovalMode,
    fallbackMode,
    meta.autoApproveTools ?? meta.bypass,
    fallbackToolApprovalMode(meta.toolApprovalMode, fallback),
  );
  return profileWithPending({
    collaborationMode: normalizeCollaborationMode(meta.collaborationMode, goal, fallbackMode),
    goalDraftMode: false,
    toolApprovalMode,
    tokenMode: normalizeTokenMode(meta.tokenMode),
    goal,
  });
}

function fieldValue(profile: ComposerProfile, field: ComposerProfileField): string {
  return profile[field];
}

function assignField(profile: ComposerProfile, field: ComposerProfileField, value: string) {
  switch (field) {
    case "collaborationMode":
      profile.collaborationMode = value as CollaborationMode;
      return;
    case "toolApprovalMode":
      profile.toolApprovalMode = value as ToolApprovalMode;
      return;
    case "tokenMode":
      profile.tokenMode = value as TokenMode;
      return;
    case "goal":
      profile.goal = value;
      return;
  }
}

function profilesEqual(a: ComposerProfile | undefined, b: ComposerProfile | undefined): boolean {
  if (!a || !b) return a === b;
  return a.collaborationMode === b.collaborationMode
    && a.goalDraftMode === b.goalDraftMode
    && a.toolApprovalMode === b.toolApprovalMode
    && a.tokenMode === b.tokenMode
    && a.goal === b.goal
    && profileFields.every((field) => Boolean(a.pending[field]) === Boolean(b.pending[field]));
}

export function reconcileComposerProfile(current: ComposerProfile | undefined, backend: ComposerProfile): ComposerProfile {
  if (!current) return { ...backend, pending: {} };

  const pending: ComposerProfilePending = {};
  const next: ComposerProfile = { ...backend, pending };

  for (const field of profileFields) {
    if (!current.pending[field]) continue;
    if (fieldValue(current, field) === fieldValue(backend, field)) continue;
    pending[field] = true;
    assignField(next, field, fieldValue(current, field));
  }

  if (current.goalDraftMode && !backend.goal && !next.goal) {
    next.goalDraftMode = true;
  }
  if (next.goal) {
    next.goalDraftMode = false;
  }

  return next;
}

export function hydrateComposerProfilesFromTabs(current: ComposerProfilesByTab, tabs: TabMeta[]): ComposerProfilesByTab {
  const next: ComposerProfilesByTab = {};
  let changed = false;

  for (const tab of tabs) {
    const profile = reconcileComposerProfile(current[tab.id], composerProfileFromTab(tab, current[tab.id]?.toolApprovalMode));
    next[tab.id] = profile;
    if (!profilesEqual(current[tab.id], profile)) changed = true;
  }

  for (const id of Object.keys(current)) {
    if (!next[id]) changed = true;
  }

  return changed ? next : current;
}

export function hydrateComposerProfileFromMeta(current: ComposerProfilesByTab, tabId: string, meta: Meta): ComposerProfilesByTab {
  const previous = current[tabId];
  const backend = composerProfileFromMeta(
    meta,
    previous ? composerProfileMode(previous) : undefined,
    previous?.toolApprovalMode,
  );
  const profile = reconcileComposerProfile(previous, backend);
  if (profilesEqual(previous, profile)) return current;
  return { ...current, [tabId]: profile };
}

export function patchComposerProfile(
  current: ComposerProfilesByTab,
  tabId: string,
  base: ComposerProfile | undefined,
  patch: Partial<Omit<ComposerProfile, "pending">>,
  pendingFields: ComposerProfileField[],
): ComposerProfilesByTab {
  const previous = current[tabId] ?? base ?? defaultComposerProfile;
  const pending: ComposerProfilePending = { ...previous.pending };
  for (const field of pendingFields) pending[field] = true;
  const profile: ComposerProfile = {
    ...previous,
    ...patch,
    pending,
  };
  if (profile.goal) {
    profile.goalDraftMode = false;
  }
  if (profilesEqual(previous, profile)) return current;
  return { ...current, [tabId]: profile };
}

export function composerProfileMode(profile: ComposerProfile): Mode {
  return modeFromAxes(profile.collaborationMode === "plan", profile.toolApprovalMode === "yolo");
}

export function displayedComposerProfileCollaborationMode(profile: ComposerProfile): CollaborationMode {
  if (profile.goalDraftMode) return "goal";
  return profile.collaborationMode;
}

export function controllerComposerProfileCollaborationMode(profile: ComposerProfile): CollaborationMode {
  const displayed = displayedComposerProfileCollaborationMode(profile);
  return displayed === "goal" && !profile.goal ? "normal" : displayed;
}

export function composerProfileWithMode(mode: Mode): Partial<Omit<ComposerProfile, "pending">> {
  return {
    collaborationMode: modeHasPlan(mode) ? "plan" : "normal",
    goalDraftMode: false,
    toolApprovalMode: modeHasAutoApproveTools(mode) ? "yolo" : "ask",
    goal: "",
  };
}

export function updateUserPlanModeIntent(
  current: UserPlanModeIntents,
  tabId: string | null | undefined,
  enabled: boolean,
): UserPlanModeIntents {
  if (!tabId) return current;
  if (enabled) {
    return current[tabId] ? current : { ...current, [tabId]: true };
  }
  if (!current[tabId]) return current;
  const next = { ...current };
  delete next[tabId];
  return next;
}

export function pruneUserPlanModeIntents(current: UserPlanModeIntents, tabIds: Iterable<string>): UserPlanModeIntents {
  const live = new Set(tabIds);
  let changed = false;
  const next: UserPlanModeIntents = {};
  for (const tabId of Object.keys(current)) {
    if (live.has(tabId)) {
      next[tabId] = true;
    } else {
      changed = true;
    }
  }
  return changed ? next : current;
}

export function shouldRestoreUserPlanMode(current: UserPlanModeIntents, tabId: string | null | undefined): boolean {
  return Boolean(tabId && current[tabId]);
}

export function resolvePlanRestoreTabId(eventTabId: string | null | undefined, activeTabId: string | null | undefined): string | null {
  return eventTabId || activeTabId || null;
}

export function shouldRestoreUserPlanModeForProfile(
  current: UserPlanModeIntents,
  tabId: string | null | undefined,
  profile?: Pick<ComposerProfile, "goal"> | null,
): boolean {
  return shouldRestoreUserPlanMode(current, tabId) && !profile?.goal.trim();
}
