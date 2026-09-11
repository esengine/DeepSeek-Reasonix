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
  const { tabs, activeTabId, meta, yoloRestoreRef, planIntentsRef, setOrder, setProfiles } = input;
  useEffect(() => {
    const ids = tabs.map((tab) => tab.id);
    setOrder((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) if (!next.includes(id)) next.push(id);
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
    const present = new Set(ids);
    for (const id of Object.keys(yoloRestoreRef.current)) {
      if (!present.has(id)) delete yoloRestoreRef.current[id];
    }
    planIntentsRef.current = pruneUserPlanModeIntents(planIntentsRef.current, present);
    setProfiles((current) => hydrateComposerProfilesFromTabs(current, [...tabs]));
  }, [planIntentsRef, setOrder, setProfiles, tabs, yoloRestoreRef]);

  useEffect(() => {
    if (!activeTabId || !meta) return;
    setProfiles((current) => hydrateComposerProfileFromMeta(current, activeTabId, meta));
  }, [activeTabId, meta, setProfiles]);
}
