import { useMemo, type Dispatch, type SetStateAction } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useRemoteComposerProfileSync } from "../lib/useRemoteComposerIntegration";
import {
  composerProfileFromMeta,
  composerProfileFromTab,
  composerProfileMode,
  defaultComposerProfile,
  displayedComposerProfileCollaborationMode,
  patchComposerProfile,
  updateUserPlanModeIntent,
  type ComposerProfile,
  type ComposerProfileField,
  type UserPlanModeIntents,
} from "../lib/composerProfile";
import type { Meta, QualityFloor, TabMeta } from "../lib/types";
import type { RemoteSessionApi } from "../lib/useRemoteSession";

export type ComposerProfileProjectionInput = {
  activeTabId: string | undefined;
  activeTab: TabMeta | undefined;
  meta: Meta | null | undefined;
  profilesByTab: Record<string, ComposerProfile>;
  setProfilesByTab: Dispatch<SetStateAction<Record<string, ComposerProfile>>>;
  tabMetas: readonly TabMeta[];
  remote: boolean;
  remoteSession: RemoteSessionApi;
  planIntentsRef: { current: UserPlanModeIntents };
  setControllerQualityFloor: (floor: QualityFloor) => Promise<unknown>;
  showToast: (message: string, level: "error") => void;
};

/**
 * Owns the active composer profile projection (UI override over the backend
 * profile, remote sync) and the profile patch commands: generic per-tab
 * patches, quality-floor application and goal activation patches. Mode axis
 * changes stay in useComposerModeActions; this hook owns the profile record.
 */
export function useComposerProfileProjection(input: ComposerProfileProjectionInput) {
  const { activeTabId, activeTab, meta, profilesByTab, setProfilesByTab, tabMetas, remote, remoteSession } = input;
  const activeComposerProfile = activeTabId ? profilesByTab[activeTabId] : undefined;
  const backendActiveComposerProfile = useMemo(() => {
    if (meta) {
      return composerProfileFromMeta(
        meta,
        activeTab ? composerProfileMode(composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode)) : undefined,
        activeComposerProfile?.toolApprovalMode,
      );
    }
    return composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode);
  }, [activeComposerProfile?.toolApprovalMode, activeTab, meta]);
  const composerProfile = activeTabId
    ? activeComposerProfile ?? backendActiveComposerProfile
    : defaultComposerProfile;
  const goal = composerProfile.goal;
  const collaborationMode = displayedComposerProfileCollaborationMode(composerProfile);
  const toolApprovalMode = composerProfile.toolApprovalMode;
  const remoteComposerProfileReady = useRemoteComposerProfileSync({ activeTabId, remote,
    remoteProfile: remoteSession.composerProfile, collaborationMode, toolApprovalMode, goal,
    qualityFloor: composerProfile.qualityFloor, pending: composerProfile.pending, setProfiles: setProfilesByTab });

  const patchActiveComposerProfile = useCommittedCommand((patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
    if (!activeTabId) return;
    setProfilesByTab((current) => patchComposerProfile(current, activeTabId, composerProfile, patch, pendingFields));
  });
  const patchComposerProfileForTab = useCommittedCommand((tabId: string, patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
    if (!tabId) return;
    setProfilesByTab((current) => {
      const base = current[tabId] ?? composerProfileFromTab(tabMetas.find((tab) => tab.id === tabId));
      return patchComposerProfile(current, tabId, base, patch, pendingFields);
    });
  });

  const applyQualityFloor = useCommittedCommand((floor: QualityFloor) => {
    if (!activeTabId) return;
    if (remote) {
      void remoteSession.setQualityFloor(floor).catch((error) => input.showToast(error instanceof Error ? error.message : String(error), "error"));
      return;
    }
    patchActiveComposerProfile({ qualityFloor: floor }, ["qualityFloor"]);
    void input.setControllerQualityFloor(floor);
  });

  const patchActivatedGoalForTab = useCommittedCommand((tabId: string, nextGoal: string): void => {
    const trimmed = nextGoal.trim();
    patchComposerProfileForTab(tabId, {
      collaborationMode: trimmed ? "goal" : "normal",
      goalDraftMode: false,
      goal: trimmed,
    }, ["collaborationMode", "goal"]);
    input.planIntentsRef.current = updateUserPlanModeIntent(input.planIntentsRef.current, tabId, false);
  });

  return {
    composerProfile,
    goal,
    collaborationMode,
    toolApprovalMode,
    remoteComposerProfileReady,
    patchActiveComposerProfile,
    patchComposerProfileForTab,
    applyQualityFloor,
    patchActivatedGoalForTab,
  };
}
