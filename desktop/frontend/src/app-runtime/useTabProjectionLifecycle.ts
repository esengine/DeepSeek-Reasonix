import { useEffect, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import type { TabMeta } from "../lib/types";
import { hydrateComposerProfileFromMeta, hydrateComposerProfilesFromTabs, pruneUserPlanModeIntents, type ComposerProfile, type UserPlanModeIntents } from "../lib/composerProfile";
import type { RestorableToolApprovalMode } from "../lib/toolApprovalMode";

export function useTabProjectionLifecycle(input: {
  tabs: readonly TabMeta[];
  activeTabId?: string | null;
  activeMeta: TabMeta | null | undefined;
  meta: Parameters<typeof hydrateComposerProfileFromMeta>[2] | null | undefined;
  yoloRestoreRef: MutableRefObject<Record<string, RestorableToolApprovalMode>>;
  planIntentsRef: MutableRefObject<UserPlanModeIntents>;
  setOrder: Dispatch<SetStateAction<string[]>>;
  setProfiles: Dispatch<SetStateAction<Record<string, ComposerProfile>>>;
}) {
  useEffect(() => {
    const ids = input.tabs.map((tab) => tab.id);
    input.setOrder((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) if (!next.includes(id)) next.push(id);
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
    const present = new Set(ids);
    for (const id of Object.keys(input.yoloRestoreRef.current)) {
      if (!present.has(id)) delete input.yoloRestoreRef.current[id];
    }
    input.planIntentsRef.current = pruneUserPlanModeIntents(input.planIntentsRef.current, present);
    input.setProfiles((current) => hydrateComposerProfilesFromTabs(current, [...input.tabs]));
  }, [input.setOrder, input.setProfiles, input.tabs]);

  useEffect(() => {
    if (!input.activeTabId || !input.meta) return;
    input.setProfiles((current) => hydrateComposerProfileFromMeta(current, input.activeTabId!, input.meta!));
  }, [input.activeTabId, input.meta, input.setProfiles]);
}
